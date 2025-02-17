package backtest

import (
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thrasher-corp/gocryptotrader/backtester/common"
	"github.com/thrasher-corp/gocryptotrader/backtester/config"
	"github.com/thrasher-corp/gocryptotrader/backtester/data"
	"github.com/thrasher-corp/gocryptotrader/backtester/data/kline"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/eventholder"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/exchange"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/portfolio"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/portfolio/risk"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/portfolio/size"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/statistics"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/statistics/currencystatistics"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/strategies"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/strategies/base"
	"github.com/thrasher-corp/gocryptotrader/backtester/eventhandlers/strategies/dollarcostaverage"
	"github.com/thrasher-corp/gocryptotrader/backtester/report"
	"github.com/thrasher-corp/gocryptotrader/currency"
	"github.com/thrasher-corp/gocryptotrader/engine"
	gctexchange "github.com/thrasher-corp/gocryptotrader/exchanges"
	"github.com/thrasher-corp/gocryptotrader/exchanges/asset"
	gctkline "github.com/thrasher-corp/gocryptotrader/exchanges/kline"
)

const testExchange = "binance"

func newBotWithExchange() (*engine.Engine, gctexchange.IBotExchange) {
	bot, err := engine.NewFromSettings(&engine.Settings{
		ConfigFile:   filepath.Join("..", "..", "testdata", "configtest.json"),
		EnableDryRun: true,
	}, nil)
	if err != nil {
		log.Fatal(err)
	}
	err = bot.LoadExchange(testExchange, false, nil)
	if err != nil {
		log.Fatal(err)
	}
	exch := bot.GetExchangeByName(testExchange)
	if exch == nil {
		log.Fatal("expected not nil")
	}
	return bot, exch
}

func TestNewFromConfig(t *testing.T) {
	t.Parallel()
	_, err := NewFromConfig(nil, "", "", nil)
	if err == nil {
		t.Error("expected error for nil config")
	}

	cfg := &config.Config{
		GoCryptoTraderConfigPath: filepath.Join("..", "..", "testdata", "configtest.json"),
	}
	_, err = NewFromConfig(cfg, "", "", nil)
	if !errors.Is(err, errNilBot) {
		t.Errorf("expected: %v, received %v", errNilBot, err)
	}

	bot, _ := newBotWithExchange()
	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, config.ErrNoCurrencySettings) {
		t.Errorf("expected: %v, received %v", config.ErrNoCurrencySettings, err)
	}

	cfg.CurrencySettings = []config.CurrencySettings{
		{
			ExchangeName: "test",
			Base:         "test",
			Quote:        "test",
		},
	}
	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, config.ErrBadInitialFunds) {
		t.Errorf("expected: %v, received %v", config.ErrBadInitialFunds, err)
	}

	cfg.CurrencySettings[0].InitialFunds = 1337
	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, config.ErrUnsetAsset) {
		t.Errorf("expected: %v, received %v", config.ErrUnsetAsset, err)
	}

	cfg.CurrencySettings[0].Asset = asset.Spot.String()
	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, engine.ErrExchangeNotFound) {
		t.Errorf("expected: %v, received %v", engine.ErrExchangeNotFound, err)
	}

	cfg.CurrencySettings[0].ExchangeName = testExchange
	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, errNoDataSource) {
		t.Errorf("expected: %v, received %v", errNoDataSource, err)
	}

	cfg.CurrencySettings[0].Base = "BTC"
	cfg.CurrencySettings[0].Quote = "USDT"

	cfg.DataSettings.APIData = &config.APIData{
		StartDate: time.Time{},
		EndDate:   time.Time{},
	}

	_, err = NewFromConfig(cfg, "", "", bot)
	if err != nil && !strings.Contains(err.Error(), "unrecognised dataType") {
		t.Error(err)
	}
	cfg.DataSettings.DataType = common.CandleStr
	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, config.ErrStartEndUnset) {
		t.Errorf("expected: %v, received %v", config.ErrStartEndUnset, err)
	}

	cfg.DataSettings.APIData.StartDate = time.Now().Add(-time.Hour)
	cfg.DataSettings.APIData.EndDate = time.Now()
	cfg.DataSettings.APIData.InclusiveEndDate = true
	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, errIntervalUnset) {
		t.Errorf("expected: %v, received %v", errIntervalUnset, err)
	}

	cfg.DataSettings.Interval = gctkline.FifteenMin.Duration()

	_, err = NewFromConfig(cfg, "", "", bot)
	if !errors.Is(err, base.ErrStrategyNotFound) {
		t.Errorf("expected: %v, received %v", base.ErrStrategyNotFound, err)
	}

	cfg.StrategySettings = config.StrategySettings{
		Name: dollarcostaverage.Name,
		CustomSettings: map[string]interface{}{
			"hello": "moto",
		},
	}
	cfg.CurrencySettings[0].MakerFee = 1337
	cfg.CurrencySettings[0].TakerFee = 1337
	_, err = NewFromConfig(cfg, "", "", bot)
	if err != nil {
		t.Error(err)
	}
}

func TestLoadData(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		GoCryptoTraderConfigPath: filepath.Join("..", "..", "testdata", "configtest.json"),
	}
	cfg.CurrencySettings = []config.CurrencySettings{
		{
			ExchangeName: "test",
			Asset:        "test",
			Base:         "test",
			Quote:        "test",
		},
	}
	cfg.CurrencySettings[0].ExchangeName = testExchange
	cfg.CurrencySettings[0].Asset = asset.Spot.String()
	cfg.CurrencySettings[0].Base = "BTC"
	cfg.CurrencySettings[0].Quote = "USDT"
	cfg.CurrencySettings[0].InitialFunds = 1337
	cfg.DataSettings.APIData = &config.APIData{
		StartDate: time.Time{},
		EndDate:   time.Time{},
	}
	cfg.DataSettings.APIData.StartDate = time.Now().Add(-time.Hour)
	cfg.DataSettings.APIData.EndDate = time.Now()
	cfg.DataSettings.Interval = gctkline.FifteenMin.Duration()
	cfg.DataSettings.DataType = common.CandleStr
	cfg.StrategySettings = config.StrategySettings{
		Name: dollarcostaverage.Name,
		CustomSettings: map[string]interface{}{
			"hello": "moto",
		},
	}
	cfg.CurrencySettings[0].MakerFee = 1337
	cfg.CurrencySettings[0].TakerFee = 1337
	bot, exch := newBotWithExchange()

	_, err := NewFromConfig(cfg, "", "", bot)
	if err != nil {
		t.Error(err)
	}
	bt := BackTest{
		Reports: &report.Data{},
	}

	cp := currency.NewPair(currency.BTC, currency.USDT)
	_, err = bt.loadData(cfg, exch, cp, asset.Spot)
	if err != nil {
		t.Error(err)
	}

	cfg.DataSettings.APIData = nil
	cfg.DataSettings.DatabaseData = &config.DatabaseData{
		StartDate:        time.Now().Add(-time.Hour),
		EndDate:          time.Now(),
		ConfigOverride:   nil,
		InclusiveEndDate: true,
	}
	cfg.DataSettings.DataType = common.CandleStr
	cfg.DataSettings.Interval = gctkline.FifteenMin.Duration()

	bt.Bot = bot
	_, err = bt.loadData(cfg, exch, cp, asset.Spot)
	if err != nil && !strings.Contains(err.Error(), "unable to retrieve data from GoCryptoTrader database") {
		t.Error(err)
	}

	cfg.DataSettings.DatabaseData = nil
	cfg.DataSettings.CSVData = &config.CSVData{
		FullPath: "test",
	}
	_, err = bt.loadData(cfg, exch, cp, asset.Spot)
	if err != nil && !strings.Contains(err.Error(), "The system cannot find the file specified.") {
		t.Error(err)
	}
	cfg.DataSettings.CSVData = nil
	cfg.DataSettings.LiveData = &config.LiveData{
		APIKeyOverride:      "test",
		APISecretOverride:   "test",
		APIClientIDOverride: "test",
		API2FAOverride:      "test",
		RealOrders:          true,
	}
	_, err = bt.loadData(cfg, exch, cp, asset.Spot)
	if err != nil {
		t.Error(err)
	}
}

func TestLoadDatabaseData(t *testing.T) {
	t.Parallel()
	cp := currency.NewPair(currency.BTC, currency.USDT)
	_, err := loadDatabaseData(nil, "", cp, "", -1)
	if err != nil && !strings.Contains(err.Error(), "nil config data received") {
		t.Error(err)
	}
	cfg := &config.Config{
		DataSettings: config.DataSettings{
			DatabaseData: &config.DatabaseData{
				StartDate:      time.Time{},
				EndDate:        time.Time{},
				ConfigOverride: nil,
			},
		},
		GoCryptoTraderConfigPath: filepath.Join("..", "..", "testdata", "configtest.json"),
	}
	_, err = loadDatabaseData(cfg, "", cp, "", -1)
	if !errors.Is(err, config.ErrStartEndUnset) {
		t.Errorf("expected %v, received %v", config.ErrStartEndUnset, err)
	}
	cfg.DataSettings.DatabaseData.StartDate = time.Now().Add(-time.Hour)
	cfg.DataSettings.DatabaseData.EndDate = time.Now()
	_, err = loadDatabaseData(cfg, "", cp, "", -1)
	if !errors.Is(err, errIntervalUnset) {
		t.Errorf("expected %v, received %v", errIntervalUnset, err)
	}

	cfg.DataSettings.Interval = gctkline.OneDay.Duration()
	_, err = loadDatabaseData(cfg, "", cp, "", -1)
	if err != nil && !strings.Contains(err.Error(), "could not retrieve database data") {
		t.Error(err)
	}

	cfg.DataSettings.DataType = common.CandleStr
	_, err = loadDatabaseData(cfg, "", cp, "", common.DataCandle)
	if err != nil && !strings.Contains(err.Error(), "exchange, base, quote, asset, interval, start & end cannot be empty") {
		t.Error(err)
	}
	_, err = loadDatabaseData(cfg, testExchange, cp, asset.Spot, common.DataCandle)
	if err != nil && !strings.Contains(err.Error(), "database support is disabled") {
		t.Error(err)
	}
}

func TestLoadLiveData(t *testing.T) {
	t.Parallel()
	err := loadLiveData(nil, nil)
	if !errors.Is(err, common.ErrNilArguments) {
		t.Error(err)
	}
	cfg := &config.Config{
		GoCryptoTraderConfigPath: filepath.Join("..", "..", "testdata", "configtest.json"),
	}
	err = loadLiveData(cfg, nil)
	if !errors.Is(err, common.ErrNilArguments) {
		t.Error(err)
	}
	b := &gctexchange.Base{
		Name: testExchange,
		API: gctexchange.API{
			AuthenticatedSupport:          false,
			AuthenticatedWebsocketSupport: false,
			PEMKeySupport:                 false,
			Credentials: struct {
				Key      string
				Secret   string
				ClientID string
				PEMKey   string
			}{},
			CredentialsValidator: struct {
				RequiresPEM                bool
				RequiresKey                bool
				RequiresSecret             bool
				RequiresClientID           bool
				RequiresBase64DecodeSecret bool
			}{
				RequiresPEM:                true,
				RequiresKey:                true,
				RequiresSecret:             true,
				RequiresClientID:           true,
				RequiresBase64DecodeSecret: true,
			},
		},
	}
	err = loadLiveData(cfg, b)
	if !errors.Is(err, common.ErrNilArguments) {
		t.Error(err)
	}
	cfg.DataSettings.LiveData = &config.LiveData{

		RealOrders: true,
	}
	cfg.DataSettings.Interval = gctkline.OneDay.Duration()
	cfg.DataSettings.DataType = common.CandleStr
	err = loadLiveData(cfg, b)
	if err != nil {
		t.Error(err)
	}

	cfg.DataSettings.LiveData.APIKeyOverride = "1234"
	cfg.DataSettings.LiveData.APISecretOverride = "1234"
	cfg.DataSettings.LiveData.APIClientIDOverride = "1234"
	cfg.DataSettings.LiveData.API2FAOverride = "1234"
	err = loadLiveData(cfg, b)
	if err != nil {
		t.Error(err)
	}
}

func TestReset(t *testing.T) {
	t.Parallel()
	bt := BackTest{
		Bot:        &engine.Engine{},
		shutdown:   make(chan struct{}),
		Datas:      &data.HandlerPerCurrency{},
		Strategy:   &dollarcostaverage.Strategy{},
		Portfolio:  &portfolio.Portfolio{},
		Exchange:   &exchange.Exchange{},
		Statistic:  &statistics.Statistic{},
		EventQueue: &eventholder.Holder{},
		Reports:    &report.Data{},
	}
	bt.Reset()
	if bt.Bot != nil {
		t.Error("expected nil")
	}
}

func TestFullCycle(t *testing.T) {
	t.Parallel()
	ex := testExchange
	cp := currency.NewPair(currency.BTC, currency.USD)
	a := asset.Spot
	tt := time.Now()

	stats := &statistics.Statistic{}
	stats.ExchangeAssetPairStatistics = make(map[string]map[asset.Item]map[currency.Pair]*currencystatistics.CurrencyStatistic)
	stats.ExchangeAssetPairStatistics[ex] = make(map[asset.Item]map[currency.Pair]*currencystatistics.CurrencyStatistic)
	stats.ExchangeAssetPairStatistics[ex][a] = make(map[currency.Pair]*currencystatistics.CurrencyStatistic)

	port, err := portfolio.Setup(&size.Size{
		BuySide:  config.MinMax{},
		SellSide: config.MinMax{},
	}, &risk.Risk{}, 0)
	if err != nil {
		t.Error(err)
	}
	_, err = port.SetupCurrencySettingsMap(ex, a, cp)
	if err != nil {
		t.Error(err)
	}
	err = port.SetInitialFunds(ex, a, cp, 1333337)
	if err != nil {
		t.Error(err)
	}
	bot, _ := newBotWithExchange()

	bt := BackTest{
		Bot:        bot,
		shutdown:   nil,
		Datas:      &data.HandlerPerCurrency{},
		Strategy:   &dollarcostaverage.Strategy{},
		Portfolio:  port,
		Exchange:   &exchange.Exchange{},
		Statistic:  stats,
		EventQueue: &eventholder.Holder{},
		Reports:    &report.Data{},
	}

	bt.Datas.Setup()
	k := kline.DataFromKline{
		Item: gctkline.Item{
			Exchange: ex,
			Pair:     cp,
			Asset:    a,
			Interval: gctkline.FifteenMin,
			Candles: []gctkline.Candle{{
				Time:   tt,
				Open:   1337,
				High:   1337,
				Low:    1337,
				Close:  1337,
				Volume: 1337,
			}},
		},
		Base: data.Base{},
		Range: gctkline.IntervalRangeHolder{
			Start: gctkline.CreateIntervalTime(tt),
			End:   gctkline.CreateIntervalTime(tt.Add(gctkline.FifteenMin.Duration())),
			Ranges: []gctkline.IntervalRange{
				{
					Start: gctkline.CreateIntervalTime(tt),
					End:   gctkline.CreateIntervalTime(tt.Add(gctkline.FifteenMin.Duration())),
					Intervals: []gctkline.IntervalData{
						{
							Start:   gctkline.CreateIntervalTime(tt),
							End:     gctkline.CreateIntervalTime(tt.Add(gctkline.FifteenMin.Duration())),
							HasData: true,
						},
					},
				},
			},
		},
	}
	err = k.Load()
	if err != nil {
		t.Error(err)
	}
	bt.Datas.SetDataForCurrency(ex, a, cp, &k)

	err = bt.Run()
	if err != nil {
		t.Error(err)
	}
}

func TestStop(t *testing.T) {
	t.Parallel()
	bt := BackTest{shutdown: make(chan struct{})}
	bt.Stop()
}

func TestFullCycleMulti(t *testing.T) {
	t.Parallel()
	ex := testExchange
	cp := currency.NewPair(currency.BTC, currency.USD)
	a := asset.Spot
	tt := time.Now()

	stats := &statistics.Statistic{}
	stats.ExchangeAssetPairStatistics = make(map[string]map[asset.Item]map[currency.Pair]*currencystatistics.CurrencyStatistic)
	stats.ExchangeAssetPairStatistics[ex] = make(map[asset.Item]map[currency.Pair]*currencystatistics.CurrencyStatistic)
	stats.ExchangeAssetPairStatistics[ex][a] = make(map[currency.Pair]*currencystatistics.CurrencyStatistic)

	port, err := portfolio.Setup(&size.Size{
		BuySide:  config.MinMax{},
		SellSide: config.MinMax{},
	}, &risk.Risk{}, 0)
	if err != nil {
		t.Error(err)
	}
	_, err = port.SetupCurrencySettingsMap(ex, a, cp)
	if err != nil {
		t.Error(err)
	}
	err = port.SetInitialFunds(ex, a, cp, 1333337)
	if err != nil {
		t.Error(err)
	}
	bot, _ := newBotWithExchange()

	bt := BackTest{
		Bot:        bot,
		shutdown:   nil,
		Datas:      &data.HandlerPerCurrency{},
		Portfolio:  port,
		Exchange:   &exchange.Exchange{},
		Statistic:  stats,
		EventQueue: &eventholder.Holder{},
		Reports:    &report.Data{},
	}

	bt.Strategy, err = strategies.LoadStrategyByName(dollarcostaverage.Name, true)
	if err != nil {
		t.Error(err)
	}

	bt.Datas.Setup()
	k := kline.DataFromKline{
		Item: gctkline.Item{
			Exchange: ex,
			Pair:     cp,
			Asset:    a,
			Interval: gctkline.FifteenMin,
			Candles: []gctkline.Candle{{
				Time:   tt,
				Open:   1337,
				High:   1337,
				Low:    1337,
				Close:  1337,
				Volume: 1337,
			}},
		},
		Base: data.Base{},
		Range: gctkline.IntervalRangeHolder{
			Start: gctkline.CreateIntervalTime(tt),
			End:   gctkline.CreateIntervalTime(tt.Add(gctkline.FifteenMin.Duration())),
			Ranges: []gctkline.IntervalRange{
				{
					Start: gctkline.CreateIntervalTime(tt),
					End:   gctkline.CreateIntervalTime(tt.Add(gctkline.FifteenMin.Duration())),
					Intervals: []gctkline.IntervalData{
						{
							Start:   gctkline.CreateIntervalTime(tt),
							End:     gctkline.CreateIntervalTime(tt.Add(gctkline.FifteenMin.Duration())),
							HasData: true,
						},
					},
				},
			},
		},
	}
	err = k.Load()
	if err != nil {
		t.Error(err)
	}

	bt.Datas.SetDataForCurrency(ex, a, cp, &k)

	err = bt.Run()
	if err != nil {
		t.Error(err)
	}
}
