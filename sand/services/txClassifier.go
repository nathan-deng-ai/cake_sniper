package services

import (
	"dark_forester/global"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// act as a switch in TxClassifier workflow when we are performing a sandwich attack.
// Alter the behaviour of the sandwicher if another bot tries to fuck ours during the sandwich attack.
// 这个参数的作用是当有其他的三明治交易被发现的时候，去改变自身三明治交易的内容，这个是一个很重要的功能。
// 下面的逻辑会有bug， 如果设置成true的花，所以只能设置成false 。
var SANDWICHWATCHDOG = false

// allow atomic treatment of Pancakeswap pending tx
var UNISWAPBLOCK = false

// sniping is considered as a one time event. Lock the fuctionality once a snipe occured
var SNIPEBLOCK = true

// only useful for sandwicher
var FRONTRUNNINGWATCHDOGBLOCK = false

// only useful for sandwicher
var SomeoneTryToFuckMe = make(chan struct{}, 1)

// only useful for sandwicher

// Core classifier to tag txs in the mempool before they're executed. Only used for PCS tx for now but other filters could be added
// 这是所有的tx 的总入口。
func TxClassifier(tx *types.Transaction, client *ethclient.Client, topSnipe chan *big.Int) {
	// 重构代码，把业务逻辑全部放入到gorouting 函数中处理

	go handleWatchedAddressTx(tx, client)

	if tx.To().Hex() == global.CAKE_ROUTER_ADDRESS {
		// fmt.Println("pancake tx", tx.Hash(), "uniswap lock", UNISWAPBLOCK)
		go handleUniswapTrade(tx, client, topSnipe)
	}

	go handle_bigtransfer(tx, client)

	if SANDWICHWATCHDOG {
		go FrontrunningWatchdog(tx, client)
	}
}

func handle_bigtransfer(tx *types.Transaction, client *ethclient.Client) {
	if tx.Value().Cmp(&global.BigTransfer) == 1 && global.BIG_BNB_TRANSFER {
		fmt.Printf("BIG TRANSFER: %v, Value: %v\n", tx.Hash().Hex(), formatEthWeiToEther(tx.Value()))
	}
}

// Alter the behaviour of the sandwicher if another bot tries to fuck ours
// during the sandwich attack.
func FrontrunningWatchdog(tx *types.Transaction, client *ethclient.Client) {
	// is executed only once
	// 这里使用了 一个lock 锁，来确保只运行一次。
	// 默认是false，非false，就是true，默认回运行。
	// 第二次是true，非true 第二次就是false， 也就不会运行了。
	// 为什么只运行一次呢？ 这里的逻辑好奇怪。
	if !FRONTRUNNINGWATCHDOGBLOCK {
		if global.ENNEMIES[*tx.To()] {
			fmt.Printf("%v trying to fuck us!", *tx.To())
			// 而且发到channel中的内容是空，这里只是做了一个信号。
			// 在处理的地方，有把这个锁设置成false的地方。
			// 这样在高并发的情况下，能够保证只有一个任务在取消。
			// 这个逻辑对吗？ 如果真的有大量的并发的时候，就只能取消一个？
			// 还是说，根本无法在同一个区块并发处理多个sandwich请求？
			SomeoneTryToFuckMe <- struct{}{}
			FRONTRUNNINGWATCHDOGBLOCK = true
		}
	}
}

// This version of the function was uniquely used for tests purposes
// as I was trying to frontrun myself on PCS. Worked like a charm!
func _handleWatchedAddressTx(tx *types.Transaction, client *ethclient.Client) {
	sender := getTxSenderAddressQuick(tx, client)
	fmt.Println("New transaction from ", sender, "(", global.AddressesWatched[sender].Name, ")")
	var swapExactETHForTokens = [4]byte{0x7f, 0xf3, 0x6a, 0xb5}
	if tx.To().Hex() == global.CAKE_ROUTER_ADDRESS {
		txFunctionHash := [4]byte{}
		copy(txFunctionHash[:], tx.Data()[:4])
		if txFunctionHash == swapExactETHForTokens {
			defer reinitBinaryResult()
			defer _reinitAnalytics()
			fmt.Println("victim tx hash :", tx.Hash())

			buildSwapETHData(tx, client)
			Rtkn0, Rbnb0 := getReservesData(client)
			if Rtkn0 == nil {
				return
			}
			BinaryResult = &BinarySearchResult{global.BASE_UNIT, global.BASE_UNIT, global.BASE_UNIT, Rtkn0, Rbnb0, big.NewInt(0)}

			sandwichingOnSteroid(tx, client)
		}
	}
}

// display transactions of the address you monitor if ADDRESS_MONITOR == true in the config file
// 这仅仅是为了显示watch list中的交易， 并没有什么特别的功能。
func handleWatchedAddressTx(tx *types.Transaction, client *ethclient.Client) {
	if global.AddressesWatched[getTxSenderAddressQuick(tx, client)].Watched {
		sender := getTxSenderAddressQuick(tx, client)
		fmt.Println("New transaction from ", sender, "(", global.AddressesWatched[sender].Name, ")")
		fmt.Println("Nonce : ", tx.Nonce())
		fmt.Println("GasPrice : ", formatEthWeiToEther(tx.GasPrice()))
		fmt.Println("Gas : ", tx.Gas()*1000000000)
		fmt.Println("Value : ", formatEthWeiToEther(tx.Value()))
		fmt.Println("To : ", tx.To())
		fmt.Println("Hash : ", tx.Hash())
	}
}
