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
	// 如果没开watchdog 才运行下面的逻辑，这个有点奇怪，
	// 不是应该开了这个开关，才有这个功能吗？ 怎么变成开了这个开关，就只有这个功能了？
	// 这个watchdog 难道是独立运行的？ 这应该不止于把？
	// 这里每一个tx 都检查一遍是否开watchdog 又什么意义吗？ 这应该是全局开关。
	if !SANDWICHWATCHDOG {
		//  这个载入seller 的动作， 应该在前面载入，而不是在这里。

		// fmt.Println("new tx to TxClassifier")

		if global.AddressesWatched[getTxSenderAddressQuick(tx, client)].Watched {
			// 如果是在watch list 列表中，就由下面的函数处理。
			// 关键问题是这里他只要监控到是这些地址，就不再到 pancakeswap的处理列表中了。
			// 这是哪门子逻辑。这应该并行处理啊。
			// 下面的几个逻辑，也是几选1，而不是所有同时处理。
			go handleWatchedAddressTx(tx, client)
		} else if tx.To().Hex() == global.CAKE_ROUTER_ADDRESS {
			// 一次只处理一个uniswap消息， 收到uniswap消息之后，就上了一个锁
			// 这会非常影响处理效率的。
			// 这里未来要改。
			fmt.Println("pancake tx", tx.Hash(), "uniswap lock", UNISWAPBLOCK)
			// 为什么这里处理3条消息之后，就不处理了呢？
			if !UNISWAPBLOCK && len(tx.Data()) >= 4 {
				// 判断是否是pancake交易。
				// pankakeSwap events are managed in their own file uniswapClassifier.go
				go handleUniswapTrade(tx, client, topSnipe)
			}
		} else if tx.Value().Cmp(&global.BigTransfer) == 1 && global.BIG_BNB_TRANSFER {
			fmt.Printf("\nBIG TRANSFER: %v, Value: %v\n", tx.Hash().Hex(), formatEthWeiToEther(tx.Value()))
		}

	} else {
		go FrontrunningWatchdog(tx, client)
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

	sender := getTxSenderAddressQuick(tx, client)
	fmt.Println("New transaction from ", sender, "(", global.AddressesWatched[sender].Name, ")")
	fmt.Println("Nonce : ", tx.Nonce())
	fmt.Println("GasPrice : ", formatEthWeiToEther(tx.GasPrice()))
	fmt.Println("Gas : ", tx.Gas()*1000000000)
	fmt.Println("Value : ", formatEthWeiToEther(tx.Value()))
	fmt.Println("To : ", tx.To())
	fmt.Println("Hash : ", tx.Hash())

}
