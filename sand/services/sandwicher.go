package services

import (
	"context"
	"dark_forester/global"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

var START time.Time

func sandwiching(tx *types.Transaction, client *ethclient.Client, SwapData UniswapExactETHToTokenInput) {
	defer _reinitAnalytics()
	START = time.Now()
	oldBalanceTrigger := global.GetTriggerWBNBBalance()
	var FirstConfirmed = make(chan *SandwichResult, 100)

	////////// SEND FRONTRUNNING TX ///////////////////

	nonce, err := client.PendingNonceAt(context.Background(), global.DARK_FORESTER_ACCOUNT.Address)
	if err != nil {
		fmt.Printf("couldn't fetch pending nonce for DARK_FORESTER_ACCOUNT", err)
	}
	signedFrontrunningTx, gasPriceFront := _prepareFrontrun(nonce, tx, client, SwapData)
	if signedFrontrunningTx == nil {
		return
	}

	SANDWICHWATCHDOG = true
	fmt.Println("Watchdog activated")
	//we  wait for vitim tx to confirm before sending backrunning tx
	go WaitRoom(client, tx.Hash(), FirstConfirmed, "frontrun")
	err = client.SendTransaction(context.Background(), signedFrontrunningTx)
	if err != nil {
		log.Fatalln("handleWatchedAddressTx: problem with frontrunning tx : ", err)
	}
	fmt.Println("Frontrunning tx hash: ", signedFrontrunningTx.Hash())
	fmt.Println("Targetted token : ", SwapData.Token)
	fmt.Println("Name : ", getTokenName(SwapData.Token, client))
	fmt.Println("pair : ", showPairAddress(SwapData))

	select {
	case <-SomeoneTryToFuckMe:
		//try to cancel the tx
		//紧急退出， 取消tx。
		// 这里对情况做了处理，其他两个地方，并没有对情况做处理。
		// 同时，他是如何判断应该取消哪一个交易的呢？
		emmmergencyCancel(nonce, client, gasPriceFront, oldBalanceTrigger, signedFrontrunningTx.Hash(), tx.Hash(), FirstConfirmed, SwapData)

	case result := <-FirstConfirmed:
		if result.Status == 0 {

			fmt.Println("frontrunning tx reverted")
			_buildFrontrunAnalytics(tx.Hash(), signedFrontrunningTx.Hash(),
				global.Nullhash, client, true, true,
				oldBalanceTrigger, gasPriceFront, SwapData)

		} else {
			fmt.Println("frontrunning tx successful. Sending backrunning..")
			sendBackRunningTx(nonce, gasPriceFront, oldBalanceTrigger, client, signedFrontrunningTx.Hash(), tx.Hash(), SwapData)
		}
	}

	// 处理一次之后， watchdog就是false了？ 就不跟踪了？
	// 锁被设置成了false， 那么下一次就可以运行了。

	// 下一次在入口逻辑中，就进入到了处理uniswap信息的逻辑中了。
	// 也就是说， 处理uniswap信息和watchdog ，同时只运行一个？
	//这里是有逻辑问题的。 默认设置 watchdog 为true， 在入口处就会进入watchdog流程。
	// 然后watchdog 流程，并没有能够把watchdog 设置为false 的地方，直接就进入死循环了。
	SANDWICHWATCHDOG = false
	FRONTRUNNINGWATCHDOGBLOCK = false
	close(FirstConfirmed)
	select {
	case <-SomeoneTryToFuckMe:
		fmt.Println("cleaning SomeoneTryToFuckMe channel")
	default:
	}

	fmt.Println("sandwiching last line")
	return
}

func emmmergencyCancel(nonce uint64, client *ethclient.Client,
	gasPriceFront, oldBalanceTrigger *big.Int, frontrunHash,
	victimHash common.Hash, FirstConfirmed chan *SandwichResult,
	SwapData UniswapExactETHToTokenInput) {

	fmt.Println("launching emmergency cancel")
	signedCancelTx := _prepareCancel(nonce, gasPriceFront)
	err := client.SendTransaction(context.Background(), signedCancelTx)
	if err != nil {
		log.Fatalln("handleWatchedAddressTx: problem with Cancel tx : ", err)
	}
	fmt.Println("Cancel tx hash: ", signedCancelTx.Hash())

	go WaitRoom(client, signedCancelTx.Hash(), FirstConfirmed, "cancel")

	var firstTxConfirmed common.Hash
	for result := range FirstConfirmed {
		if result.Status == 0 {
			fmt.Println(result.Hash, "reverted")
		} else if result.Status == 9 {
			fmt.Println(result.Hash, "couldn't fetch receipt")
		} else if result.Status == 1 {
			fmt.Println(result.Hash, "confirmed !")
			firstTxConfirmed = result.Hash
			break
		} else {
			fmt.Println(result.Hash, "unknow status:", result.Status)
		}
	}

	if firstTxConfirmed == signedCancelTx.Hash() {
		fmt.Println("Cancel tx confirmed successfully before frontrunning tx")
		_buildCancelAnalytics(victimHash, signedCancelTx.Hash(),
			client, oldBalanceTrigger,
			signedCancelTx.GasPrice(), SwapData)
	} else {
		fmt.Println("Frontrunning tx confirmed before cancel tx... launching backrunning tx")
		sendBackRunningTx(nonce, gasPriceFront, oldBalanceTrigger, client, victimHash, frontrunHash, SwapData)
	}
}

// we send backrunning tx only if frontruning succeeded and wasn't cancelled.
func sendBackRunningTx(nonce uint64, gasPriceFront,
	oldBalanceTrigger *big.Int, client *ethclient.Client,
	frontrunHash, victimHash common.Hash,
	SwapData UniswapExactETHToTokenInput) {

	signedBackrunningTx := _prepareBackrun(nonce, gasPriceFront, SwapData)
	err := client.SendTransaction(context.Background(), signedBackrunningTx)
	if err != nil {
		log.Fatalln("sendBackRunningTx: problem with backrunning tx : ", err)
	}
	fmt.Println("Backrunning tx hash: ", signedBackrunningTx.Hash())

	// check if backrunning tx succeeded:
	result := _waitForPendingState(client, signedBackrunningTx.Hash(), context.Background(), "backrun")

	if result.Status == 0 {
		// a failed backrunning tx is worrying if front succeeded. It means the stinky tokens are locked in TRIGGER and couldn't be sold back.
		// at this point, we need to shut down dark forested and rescue the tokens manually.
		fmt.Printf("\nbackrunning tx reverted. Need to manually rescue funds:\ntoken name involved : %v\nBEP20 address:%v\n", SharedAnalytic.TokenName, SharedAnalytic.TokenAddr)
		_buildFrontrunAnalytics(victimHash, frontrunHash, signedBackrunningTx.Hash(),
			client, false, true, oldBalanceTrigger,
			signedBackrunningTx.GasPrice(), SwapData)
		log.Fatalln()
	} else {
		// backrunning tx succeeded. Calculates realised profits
		fmt.Println("backrunning tx sucessful")
		_buildFrontrunAnalytics(victimHash, frontrunHash,
			signedBackrunningTx.Hash(), client, false, false,
			oldBalanceTrigger, signedBackrunningTx.GasPrice(), SwapData)
	}
}
