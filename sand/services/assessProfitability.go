package services

import (
	"dark_forester/contracts/uniswap"
	"dark_forester/global"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Equivalent of _getAmountOut function of the PCS router. Calculates z.
// 这个z值就是 x * y =k 中的k值吧。我理解。
func _getAmountOut(myMaxBuy, reserveOut, reserveIn *big.Int) *big.Int {

	var myMaxBuy9975 = new(big.Int)
	var z = new(big.Int)
	num := big.NewInt(9975)
	myMaxBuy9975.Mul(num, myMaxBuy)
	num.Mul(myMaxBuy9975, reserveOut)

	den := big.NewInt(10000)
	den.Mul(den, reserveIn)
	den.Add(den, myMaxBuy9975)
	z.Div(num, den)
	return z
}

// get reserves of a PCS pair an return it
func getReservesData(client *ethclient.Client) (*big.Int, *big.Int) {
	pairAddress, _ := global.FACTORY.GetPair(&bind.CallOpts{}, SwapData.Token, global.WBNB_ADDRESS)
	PAIR, _ := uniswap.NewIPancakePair(pairAddress, client)
	reservesData, _ := PAIR.GetReserves(&bind.CallOpts{})
	if reservesData.Reserve0 == nil {
		return nil, nil
	}
	var Rtkn0 = new(big.Int)
	var Rbnb0 = new(big.Int)
	token0, _ := PAIR.Token0(&bind.CallOpts{})
	if token0 == global.WBNB_ADDRESS {
		Rbnb0 = reservesData.Reserve0
		Rtkn0 = reservesData.Reserve1
	} else {
		Rbnb0 = reservesData.Reserve1
		Rtkn0 = reservesData.Reserve0
	}
	return Rtkn0, Rbnb0
}

// perform the binary search to determine optimal amount of WBNB
// to engage on the sandwich without breaking victim's slippage
func _binarySearch(amountToTest, Rtkn0, Rbnb0, txValue, amountOutMinVictim *big.Int) {

	// 第一笔交易的模拟。测试 amountToTest这个值，是否合理。
	// 这里的 amountToTest 我理解是我自己的购买金额，
	// tknTmBuying 是我购买的 token的数量。
	amountTknImBuying1 := _getAmountOut(amountToTest, Rtkn0, Rbnb0)
	var Rtkn1 = new(big.Int)
	var Rbnb1 = new(big.Int)

	//token reserved 和 bnb reserved 的最新数量。
	// 进行victim 购买的模拟。 第二笔交易的模拟。
	Rtkn1.Sub(Rtkn0, amountTknImBuying1)
	Rbnb1.Add(Rbnb0, amountToTest)
	amountTknVictimWillBuy1 := _getAmountOut(txValue, Rtkn1, Rbnb1)

	// check if this amountToTest is really the best we can have
	// 1) we don't break victim's slippage with amountToTest
	// 检查是否触发滑点。
	if amountTknVictimWillBuy1.Cmp(amountOutMinVictim) == 1 {
		// 用户可买的金额大于用户最小金额，也就是没有触发滑点的情况。
		// 2) engage MAXBOUND on the sandwich if MAXBOUND doesn't break slippage
		// 这里是找到最大的bound 值。这是设置的值，只要不超过，就不会有问题。
		//如果amountToTest 等于最大值，并且没有触发滑点，就直接使用最大值了。
		// 但是这个逻辑，什么时候回触发呢？ 需要把所有的值，都检查一遍，
		// 外层定义了跨度是 0.02 所以是检查了所有的0.02 跨度的值。
		// 检查到maxbound 的时候，才会触发，这效率也太低了。
		// 并没有感觉这里在binarysearch， 而是一个全部覆盖的search。

		if amountToTest.Cmp(global.MAXBOUND) == 0 {

			// 这里直接设置，然后return 空，
			BinaryResult = &BinarySearchResult{
				global.MAXBOUND,         //买入值。
				amountTknImBuying1,      // 买到的token的数量。
				amountTknVictimWillBuy1, // victim 购买值
				Rtkn1, Rbnb1,
				big.NewInt(0)}
			// 这里为什么不return BinaryResult 而是直接操作了一个全局变量？
			return
		}

		// 这里是将amountToTest 加上了BASE_UNIT，也就是加上了0.02bnb
		myMaxBuy := amountToTest.Add(amountToTest, global.BASE_UNIT)

		// 使用加上0.02 的值，再模拟一遍。
		amountTknImBuying2 := _getAmountOut(myMaxBuy, Rtkn0, Rbnb0)
		var Rtkn1Test = new(big.Int)
		var Rbnb1Test = new(big.Int)
		Rtkn1Test.Sub(Rtkn0, amountTknImBuying2)
		Rbnb1Test.Add(Rbnb0, myMaxBuy)

		// victim的实际购买。
		amountTknVictimWillBuy2 := _getAmountOut(txValue, Rtkn1Test, Rbnb1Test)
		// 3) if we go 1 step further on the ladder and it breaks the slippage,
		// that means that amountToTest is really the amount of WBNB that
		// we can engage and milk the maximum of profits from the sandwich.
		// 看是否触发了滑点，如果触发了，则返回上一个值，就ok了。
		// 这个算哪门子的binarySearch？

		// 并且这个值，并不是按照函数返回值的方式组织的，而是去设置一个全局变量的方式的。
		if amountTknVictimWillBuy2.Cmp(amountOutMinVictim) == -1 {
			// 这里直接设置，但是不return，
			BinaryResult = &BinarySearchResult{amountToTest, amountTknImBuying1, amountTknVictimWillBuy1, Rtkn1, Rbnb1, big.NewInt(0)}
		}
	}
	return
}

// test if we break victim's slippage with MNBOUND WBNB engaged
func _testMinbound(Rtkn, Rbnb, txValue, amountOutMinVictim *big.Int) int {

	amountTknImBuying := _getAmountOut(global.MINBOUND, Rtkn, Rbnb)
	var Rtkn1 = new(big.Int)
	var Rbnb1 = new(big.Int)
	Rtkn1.Sub(Rtkn, amountTknImBuying)
	Rbnb1.Add(Rbnb, global.MINBOUND)
	amountTknVictimWillBuy := _getAmountOut(txValue, Rtkn1, Rbnb1)
	return amountTknVictimWillBuy.Cmp(amountOutMinVictim)
}

// 这里是 binary search 的算法部分。
func getMyMaxBuyAmount2(Rtkn0, Rbnb0, txValue, amountOutMinVictim *big.Int, arrayOfInterest []*big.Int) {
	var wg = sync.WaitGroup{}
	// test with the minimum value we consent to engage.
	// If we break victim's slippage with our MINBOUND,
	// we don't go further.
	// 这里应该返回一个结构体，将所有数据记录下来，然后增加一个是否可执行的标识位
	// 现在这种做法，无法做单元测试。

	// 测试最小值是否满足，如果不满足就直接返回空BinanrySearhReault了。
	if _testMinbound(Rtkn0, Rbnb0, txValue, amountOutMinVictim) == 1 {
		// 这里是吧所有的 arrayOfInterest 值给测试一遍吗？ 这得多大的成本，
		// 而且最终的值是多少，是由什么来确定的呢？
		for _, amountToTest := range arrayOfInterest {
			wg.Add(1)
			go func() {
				// 循环调用 , 测试 amountTotest , 但是他的结果如何返回到下一轮循环中呢？
				_binarySearch(amountToTest, Rtkn0, Rbnb0, txValue, amountOutMinVictim)
				wg.Done()
			}()
			wg.Wait()
		}
		return
	} else {
		BinaryResult = &BinarySearchResult{}
	}
}

// 判断是否有利润
func assessProfitability(client *ethclient.Client,
	tkn_adddress common.Address, txValue,
	amountOutMinVictim, Rtkn0, Rbnb0 *big.Int) bool {
	var expectedProfit = new(big.Int)
	arrayOfInterest := global.SANDWICHER_LADDER

	// only purpose of this function is to complete the struct BinaryResult
	// via a binary search performed on the sandwich ladder we initialised
	// in the config file.
	// If we cannot even buy 1 BNB without breaking victim slippage,
	// BinaryResult will be nil
	// 这里不需要设置一个 1BNB的条件，只要计算出来有利润，就是可以做的。
	getMyMaxBuyAmount2(Rtkn0, Rbnb0, txValue, amountOutMinVictim, arrayOfInterest)

	if BinaryResult.MaxBNBICanBuy != nil {
		var Rtkn2 = new(big.Int)
		var Rbnb2 = new(big.Int)
		Rtkn2.Sub(BinaryResult.Rtkn1, BinaryResult.AmountTknVictimWillBuy)
		Rbnb2.Add(BinaryResult.Rbnb1, txValue)

		// r0 --> I buy --> r1 --> victim buy --> r2 --> i sell
		// at this point of execution, we just did r2 so the "i sell" phase remains to be done
		bnbAfterSell := _getAmountOut(BinaryResult.AmountTknIWillBuy, Rbnb2, Rtkn2)
		expectedProfit.Sub(bnbAfterSell, BinaryResult.MaxBNBICanBuy)

		if expectedProfit.Cmp(global.MINPROFIT) == 1 {
			BinaryResult.ExpectedProfits = expectedProfit
			return true
		}
	}
	return false
}

func reinitBinaryResult() {
	BinaryResult = &BinarySearchResult{}
}
