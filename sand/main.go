package main

import (
	"context"
	"dark_forester/contracts/erc20"
	"dark_forester/global"
	"dark_forester/services"
	"flag"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"

	// "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Entry point of dark_forester.
// Before Anything, check /global/config to correctly parametrize the bot

var wg = sync.WaitGroup{}
var TopSnipe = make(chan *big.Int)

// convert WEI to ETH
func formatEthWeiToEther(etherAmount *big.Int) float64 {
	var base, exponent = big.NewInt(10), big.NewInt(18)
	denominator := base.Exp(base, exponent, nil)
	tokensSentFloat := new(big.Float).SetInt(etherAmount)
	denominatorFloat := new(big.Float).SetInt(denominator)
	final, _ := new(big.Float).Quo(tokensSentFloat, denominatorFloat).Float64()
	return final
}

// fetch ERC20 token symbol
func getTokenSymbol(tokenAddress common.Address, client *ethclient.Client) string {
	tokenIntance, _ := erc20.NewErc20(tokenAddress, client)
	sym, _ := tokenIntance.Symbol(nil)
	return sym
}

// main loop of the bot
func StreamNewTxs(client *ethclient.Client, rpcClient *rpc.Client) {

	// Go channel to pipe data from client subscription
	newTxsChannel := make(chan common.Hash)

	// Subscribe to receive one time events for new txs
	// 订阅pending 的tx 。
	_, err := rpcClient.EthSubscribe(
		context.Background(), newTxsChannel, "newPendingTransactions", // no additional args
	)

	if err != nil {
		fmt.Println("error while subscribing: ", err)
	}
	fmt.Println("Subscribed to mempool txs!")

	for transactionHash := range newTxsChannel {
		go handleTransaction(transactionHash, client)
	}
}

// 这里做通用的检查， 与业务逻辑无关的检查
func handleTransaction(transactionHash common.Hash, client *ethclient.Client) {
	tx, is_pending, _ := client.TransactionByHash(context.Background(), transactionHash)
	// If tx is valid and still unconfirmed
	// 用这个打印信息来查看当前节点的响应速度。
	// fmt.Println("new pending transaction hash ", transactionHash, is_pending)
	if is_pending {
		// _, _ = signer.Sender(tx)
		services.TxClassifier(tx, client, TopSnipe)
	}

}

func pre_run_info(client *ethclient.Client, rpcClient *rpc.Client) {

	fmt.Println("////////////// BIG TRANSFERS //////////////////")
	if global.BIG_BNB_TRANSFER {
		fmt.Println("activated\nthreshold of interest : transfers >", global.BNB[:2], " BNB")
	} else {
		fmt.Println("not activated")
	}

	fmt.Println("////////////// ADDRESS MONITORING //////////////////")
	if global.ADDRESS_MONITOR {
		fmt.Println("activated\nthe following addresses are monitored : ")
		for addy, addressData := range global.AddressesWatched {
			fmt.Println("address : ", addy, "name: ", addressData.Name)
		}
	} else {
		fmt.Println("not activated")
	}

	fmt.Println("////////////// SANDWICHER //////////////////")
	if global.Sandwicher {
		fmt.Println("activated\n\nmax BNB amount authorised for one sandwich : ", global.Sandwicher_maxbound, "WBNB")
		fmt.Println("minimum profit expected : ", global.Sandwicher_minprofit, "WBNB")
		fmt.Println("current WBNB balance inside TRIGGER : ", formatEthWeiToEther(global.GetTriggerWBNBBalance()), "WBNB")
		fmt.Println("TRIGGER balance at which we stop execution : ", formatEthWeiToEther(global.STOPLOSSBALANCE), "WBNB")
		fmt.Println("WARNING: be sure TRIGGER WBNB balance is > SANDWICHER MAXBOUND !!")

		activeMarkets := 0
		// 这里只是计算一下当前active的 市场，并不是过滤的地方。
		for _, specs := range global.SANDWICH_BOOK {
			if specs.Whitelisted && !specs.ManuallyDisabled {
				// fmt.Println(specs.Name, market, specs.Liquidity)
				// 只有被标记为 白名单和 没有手动禁用， 都算作有效市场。
				// 这里可以先注视掉， 或者用黑名单模式。或者有方法可以获得白名单列表。
				// 白名单需要持续的去维护。
				activeMarkets += 1
			}
		}
		fmt.Println("Number of active Markets: ", activeMarkets, "")

		fmt.Println("Manually disabled Markets: ")
		for market, specs := range global.SANDWICH_BOOK {
			if specs.ManuallyDisabled {
				fmt.Println(specs.Name, market, specs.Liquidity)
			}
		}
		fmt.Println("Ennemies: ")
		for ennemy, _ := range global.ENNEMIES {
			fmt.Println(ennemy)
		}

	} else {
		fmt.Println("not activated")
	}

	fmt.Println("////////////// LIQUIDITY SNIPING //////////////////")
	if global.Sniping {
		fmt.Println("activated")
		name, _ := global.Snipe.Tkn.Name(&bind.CallOpts{})
		fmt.Println("token targetted: ", global.Snipe.TokenAddress, "(", name, ")")
		fmt.Println("minimum liquidity expected : ", formatEthWeiToEther(global.Snipe.MinLiq), getTokenSymbol(global.Snipe.TokenPaired, client))
		fmt.Println("current WBNB balance inside TRIGGER : ", formatEthWeiToEther(global.GetTriggerWBNBBalance()), "WBNB")

	} else {
		fmt.Println("not activated")
	}
}

func main() {

	// we say <place_holder> for the defval as it is anyway filtered to geth_ipc in the switch
	ClientEntered := flag.String("client", "xxx", "Gateway to the bsc protocol. Available options:\n\t-bsc_testnet\n\t-bsc\n\t-geth_http\n\t-geth_ipc")
	flag.Parse()

	rpcClient := services.InitRPCClient(ClientEntered)
	client := services.GetCurrentClient()

	global.InitDF(client)

	// init goroutine Clogg if global.Sniping == true
	if global.Sniping {
		wg.Add(1)
		go func() {
			services.Clogg(client, TopSnipe)
			wg.Done()
		}()
	}

	// Launch txpool streamer
	pre_run_info(client, rpcClient)
	StreamNewTxs(client, rpcClient)

}
