package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	hdwallet "github.com/ranjbar-dev/hd-wallet"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     Server     `yaml:"server"`
	Database   Database   `yaml:"database"`
	Wallet     Wallet     `yaml:"wallet"`
	Tron       Tron       `yaml:"tron"`
	Assets     []Asset    `yaml:"assets"`
	Orders     Orders     `yaml:"orders"`
	IPN        IPN        `yaml:"ipn"`
	Energy     Energy     `yaml:"energy"`
	Price      Price      `yaml:"price"`
	Resources  Resources  `yaml:"resources"`
	Withdrawal Withdrawal `yaml:"withdrawal"`
	Auth       Auth       `yaml:"auth"`
	Log        Log        `yaml:"log"`
}

type Server struct {
	Listen       string        `yaml:"listen"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type Database struct {
	Path string `yaml:"path"`
}

type Wallet struct {
	SeedFile        string        `yaml:"seed_file"`
	Account         uint32        `yaml:"account"`
	PoolInitialSize int           `yaml:"pool_initial_size"`
	PoolMinFree     int           `yaml:"pool_min_free"`
	PoolMaxSize     int           `yaml:"pool_max_size"`
	Cooldown        time.Duration `yaml:"cooldown"`
}

type Tron struct {
	Endpoints             []Endpoint    `yaml:"endpoints"`
	SolidityURL           string        `yaml:"solidity_url"`
	PollInterval          time.Duration `yaml:"poll_interval"`
	ConfirmationsRequired int           `yaml:"confirmations_required"`
	ReorgDepth            int           `yaml:"reorg_depth"`
	RequestTimeout        time.Duration `yaml:"request_timeout"`
	DailyRequestQuota     int64         `yaml:"daily_request_quota"`
}

type Endpoint struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
	Weight int    `yaml:"weight"`
}

type Asset struct {
	Symbol     string `yaml:"symbol"`
	Kind       string `yaml:"kind"`
	Contract   string `yaml:"contract"`
	Decimals   int    `yaml:"decimals"`
	MinDeposit string `yaml:"min_deposit"`
	Verified   bool   `yaml:"verified"`
}

type Orders struct {
	DefaultTTL         time.Duration `yaml:"default_ttl"`
	UnderpaymentPolicy string        `yaml:"underpayment_policy"`
	OverpaymentPolicy  string        `yaml:"overpayment_policy"`
}

type IPN struct {
	Consumers       []Consumer      `yaml:"consumers"`
	DefaultConsumer string          `yaml:"default_consumer"`
	Timeout         time.Duration   `yaml:"timeout"`
	MaxAttempts     int             `yaml:"max_attempts"`
	Workers         int             `yaml:"workers"`
	Backoff         []time.Duration `yaml:"backoff"`
}

type Consumer struct {
	Name           string `yaml:"name"`
	URL            string `yaml:"url"`
	Secret         string `yaml:"secret"`
	ReceivesGlobal bool   `yaml:"receives_global"`
	Enabled        bool   `yaml:"enabled"`
}

type Secret string

func (Secret) String() string { return "[REDACTED]" }

func (Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

type Energy struct {
	Enabled        bool          `yaml:"enabled"`
	Provider       string        `yaml:"provider"`
	APIURL         string        `yaml:"api_url"`
	APIKey         Secret        `yaml:"api_key"`
	APISecret      Secret        `yaml:"api_secret"`
	Timeout        time.Duration `yaml:"timeout"`
	RentAmount     int64         `yaml:"rent_amount"`
	RentDuration   time.Duration `yaml:"rent_duration"`
	MaxPriceTRX    string        `yaml:"max_price_trx"`
	BalanceWarnTRX string        `yaml:"balance_warn_trx"`
	PollInterval   time.Duration `yaml:"poll_interval"`
	PollTimeout    time.Duration `yaml:"poll_timeout"`
	FallbackToBurn bool          `yaml:"fallback_to_burn"`
	MaxBurnTRX     string        `yaml:"max_burn_trx"`
}

func (Energy) LogValue() slog.Value {
	return slog.StringValue("energy config (credentials redacted)")
}

type Price struct {
	Provider   string        `yaml:"provider"`
	URL        string        `yaml:"url"`
	Interval   time.Duration `yaml:"interval"`
	Pairs      []string      `yaml:"pairs"`
	StaleAfter time.Duration `yaml:"stale_after"`
}

type Resources struct {
	MinEnergy           int64         `yaml:"min_energy"`
	MinBandwidth        int64         `yaml:"min_bandwidth"`
	CheckInterval       time.Duration `yaml:"check_interval"`
	SlowCheckInterval   time.Duration `yaml:"slow_check_interval"`
	PollThresholdUSD    string        `yaml:"poll_threshold_usd"`
	MaxPolledAddresses  int           `yaml:"max_polled_addresses"`
	ResourceWalletIndex uint32        `yaml:"resource_wallet_index"`
	BandwidthStrategy   string        `yaml:"bandwidth_strategy"`
	BandwidthTopupTRX   string        `yaml:"bandwidth_topup_trx"`
}

type Withdrawal struct {
	Enabled       bool          `yaml:"enabled"`
	DailyLimitUSD string        `yaml:"daily_limit_usd"`
	FeeLimitTRX   int64         `yaml:"fee_limit_trx"`
	Expiration    time.Duration `yaml:"expiration"`
	RequireTOTP   bool          `yaml:"require_totp"`
}

type Auth struct {
	APIKeys    []APIKey `yaml:"api_keys"`
	TOTPSecret string   `yaml:"totp_secret"`
}

type APIKey struct {
	Name    string   `yaml:"name"`
	KeyHash string   `yaml:"key_hash"`
	Scopes  []string `yaml:"scopes"`
}

type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// LogValue prevents accidental credential disclosure if the full config is logged (CFG-011).
func (Config) LogValue() slog.Value { return slog.StringValue("config (secrets redacted)") }

// Load reads one strict YAML document and validates it before returning (CFG-001, CFG-004).
func Load(path string) (Config, error) {
	if err := validateFileMode(path); err != nil {
		return Config{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()

	var cfg Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("decode config: multiple YAML documents are not allowed")
		}
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return cfg, nil
}

func validateFileMode(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config: %w", err)
	}
	// Windows has ACLs rather than Unix permission bits; os.FileMode cannot express 0600 there.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		return fmt.Errorf("config mode is %04o, want 0600 (CFG-004)", info.Mode().Perm())
	}
	return nil
}

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// Validate rejects invalid or missing values instead of supplying defaults (CFG-001..015).
func (c Config) Validate() error {
	var errs []error
	add := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	positiveDuration := func(name string, d time.Duration) { add(d > 0, "%s must be positive", name) }
	decimalValue := func(name, value string, required bool) {
		add(!required && value == "" || decimal.MatchString(value), "%s must be a non-negative decimal string", name)
	}

	_, port, err := net.SplitHostPort(c.Server.Listen)
	portNumber, portErr := strconv.Atoi(port)
	add(c.Server.Listen != "" && err == nil && portErr == nil && portNumber >= 1 && portNumber <= 65535, "server.listen must be host:port with port 1..65535")
	positiveDuration("server.read_timeout", c.Server.ReadTimeout)
	positiveDuration("server.write_timeout", c.Server.WriteTimeout)
	add(c.Database.Path != "", "database.path is required")
	add(c.Wallet.SeedFile != "", "wallet.seed_file is required")
	add(c.Wallet.Account < 1<<31, "wallet.account must be below 2^31")
	add(c.Wallet.PoolInitialSize > 0 && c.Wallet.PoolInitialSize <= 1000, "wallet.pool_initial_size must be 1..1000")
	add(c.Wallet.PoolMinFree > 0 && c.Wallet.PoolMinFree <= c.Wallet.PoolMaxSize, "wallet.pool_min_free must be 1..pool_max_size")
	add(c.Wallet.PoolMaxSize >= c.Wallet.PoolInitialSize && c.Wallet.PoolMaxSize <= 1000, "wallet.pool_max_size must be pool_initial_size..1000")
	positiveDuration("wallet.cooldown", c.Wallet.Cooldown)

	add(len(c.Tron.Endpoints) > 0, "tron.endpoints must not be empty")
	hosts := make(map[string]struct{}, len(c.Tron.Endpoints))
	for i, endpoint := range c.Tron.Endpoints {
		u, urlErr := httpURL(endpoint.URL)
		add(urlErr == nil, "tron.endpoints[%d].url: %v", i, urlErr)
		add(endpoint.Weight > 0, "tron.endpoints[%d].weight must be positive", i)
		if urlErr == nil {
			host := strings.ToLower(u.Hostname())
			_, duplicate := hosts[host]
			add(!duplicate, "tron.endpoints[%d] duplicates enabled hostname %q (CFG-015)", i, host)
			hosts[host] = struct{}{}
		}
	}
	_, err = httpURL(c.Tron.SolidityURL)
	add(err == nil, "tron.solidity_url: %v", err)
	positiveDuration("tron.poll_interval", c.Tron.PollInterval)
	add(c.Tron.ConfirmationsRequired > 0, "tron.confirmations_required must be positive")
	add(c.Tron.ReorgDepth > 0, "tron.reorg_depth must be positive")
	add(c.Tron.RequestTimeout == 10*time.Second, "tron.request_timeout must be 10s (CHN-024)")
	add(c.Tron.DailyRequestQuota > 0, "tron.daily_request_quota must be positive")

	add(len(c.Assets) > 0, "assets must not be empty")
	symbols := make(map[string]struct{}, len(c.Assets))
	contracts := make(map[string]struct{}, len(c.Assets))
	for i, asset := range c.Assets {
		add(asset.Symbol != "", "assets[%d].symbol is required", i)
		_, duplicate := symbols[asset.Symbol]
		add(!duplicate, "assets[%d].symbol %q is duplicated", i, asset.Symbol)
		symbols[asset.Symbol] = struct{}{}
		add(asset.Kind == "native" || asset.Kind == "trc20", "assets[%d].kind must be native or trc20", i)
		add(asset.Decimals >= 0 && asset.Decimals <= 255, "assets[%d].decimals must be 0..255", i)
		add(asset.Verified, "assets[%d].verified must be explicitly true (CFG-014)", i)
		if asset.Kind == "trc20" {
			add(asset.Contract != "", "assets[%d].contract is required for trc20", i)
		}
		if asset.Contract != "" {
			add(hdwallet.IsValidAddress(hdwallet.TRX, asset.Contract), "assets[%d].contract is not a valid TRX address (CFG-002)", i)
			contract := strings.ToLower(asset.Contract)
			_, duplicate = contracts[contract]
			add(!duplicate, "assets[%d].contract %q is duplicated", i, asset.Contract)
			contracts[contract] = struct{}{}
		}
		decimalValue(fmt.Sprintf("assets[%d].min_deposit", i), asset.MinDeposit, false)
	}

	positiveDuration("orders.default_ttl", c.Orders.DefaultTTL)
	add(c.Orders.UnderpaymentPolicy == "partial" || c.Orders.UnderpaymentPolicy == "reject", "orders.underpayment_policy must be partial or reject")
	add(c.Orders.OverpaymentPolicy == "credit_and_log", "orders.overpayment_policy must be credit_and_log")

	consumerNames := make(map[string]Consumer, len(c.IPN.Consumers))
	consumerSecrets := make(map[string]struct{}, len(c.IPN.Consumers))
	for i, consumer := range c.IPN.Consumers {
		add(consumer.Name != "", "ipn.consumers[%d].name is required (CFG-007)", i)
		_, duplicate := consumerNames[consumer.Name]
		add(!duplicate, "ipn.consumers[%d].name %q is duplicated (CFG-007)", i, consumer.Name)
		consumerNames[consumer.Name] = consumer
		_, err = httpURL(consumer.URL)
		add(err == nil, "ipn.consumers[%d].url: %v", i, err)
		add(consumer.Secret != "", "ipn.consumers[%d].secret is required", i)
		_, duplicate = consumerSecrets[consumer.Secret]
		add(!duplicate, "ipn.consumers[%d].secret is reused (CFG-008)", i)
		consumerSecrets[consumer.Secret] = struct{}{}
	}
	defaultConsumer, ok := consumerNames[c.IPN.DefaultConsumer]
	add(ok && defaultConsumer.Enabled, "ipn.default_consumer must name an enabled consumer (CFG-009)")
	positiveDuration("ipn.timeout", c.IPN.Timeout)
	add(c.IPN.MaxAttempts > 0, "ipn.max_attempts must be positive")
	add(c.IPN.Workers > 0, "ipn.workers must be positive")
	add(len(c.IPN.Backoff) == c.IPN.MaxAttempts, "ipn.backoff must contain max_attempts entries")
	for i, d := range c.IPN.Backoff {
		positiveDuration(fmt.Sprintf("ipn.backoff[%d]", i), d)
	}

	if c.Energy.Enabled {
		add(c.Energy.Provider != "", "energy.provider is required when enabled")
		_, err = httpURL(c.Energy.APIURL)
		add(err == nil, "energy.api_url: %v", err)
		add(c.Energy.APIKey != "", "energy.api_key is required when enabled")
		add(c.Energy.APISecret != "", "energy.api_secret is required when enabled")
		positiveDuration("energy.timeout", c.Energy.Timeout)
		add(c.Energy.RentAmount > 0, "energy.rent_amount must be positive")
		positiveDuration("energy.rent_duration", c.Energy.RentDuration)
		positiveDuration("energy.poll_interval", c.Energy.PollInterval)
		positiveDuration("energy.poll_timeout", c.Energy.PollTimeout)
	}
	decimalValue("energy.max_price_trx", c.Energy.MaxPriceTRX, c.Energy.Enabled)
	decimalValue("energy.balance_warn_trx", c.Energy.BalanceWarnTRX, c.Energy.Enabled)
	decimalValue("energy.max_burn_trx", c.Energy.MaxBurnTRX, c.Energy.FallbackToBurn)

	add(c.Price.Provider != "", "price.provider is required")
	_, err = httpURL(c.Price.URL)
	add(err == nil, "price.url: %v", err)
	positiveDuration("price.interval", c.Price.Interval)
	add(len(c.Price.Pairs) > 0, "price.pairs must not be empty")
	positiveDuration("price.stale_after", c.Price.StaleAfter)

	add(c.Resources.MinEnergy >= 0, "resources.min_energy must be non-negative")
	add(c.Resources.MinBandwidth >= 0, "resources.min_bandwidth must be non-negative")
	positiveDuration("resources.check_interval", c.Resources.CheckInterval)
	positiveDuration("resources.slow_check_interval", c.Resources.SlowCheckInterval)
	decimalValue("resources.poll_threshold_usd", c.Resources.PollThresholdUSD, true)
	add(c.Resources.MaxPolledAddresses > 0, "resources.max_polled_addresses must be positive")
	add(c.Resources.ResourceWalletIndex >= 1000 && c.Resources.ResourceWalletIndex < 1<<31, "resources.resource_wallet_index must be in the operational range 1000..2^31-1 (CFG-013)")
	add(c.Resources.BandwidthStrategy == "topup" || c.Resources.BandwidthStrategy == "delegate", "resources.bandwidth_strategy must be topup or delegate")
	decimalValue("resources.bandwidth_topup_trx", c.Resources.BandwidthTopupTRX, true)

	if c.Withdrawal.Enabled {
		decimalValue("withdrawal.daily_limit_usd", c.Withdrawal.DailyLimitUSD, true)
		add(c.Withdrawal.FeeLimitTRX > 0, "withdrawal.fee_limit_trx must be positive")
		positiveDuration("withdrawal.expiration", c.Withdrawal.Expiration)
	}

	keyNames := make(map[string]struct{}, len(c.Auth.APIKeys))
	for i, key := range c.Auth.APIKeys {
		add(key.Name != "", "auth.api_keys[%d].name is required", i)
		_, duplicate := keyNames[key.Name]
		add(!duplicate, "auth.api_keys[%d].name %q is duplicated", i, key.Name)
		keyNames[key.Name] = struct{}{}
		add(strings.HasPrefix(key.KeyHash, "argon2id$") || strings.HasPrefix(key.KeyHash, "$argon2id$"), "auth.api_keys[%d].key_hash must be an Argon2id hash (CFG-003)", i)
		add(len(key.Scopes) > 0, "auth.api_keys[%d].scopes must not be empty", i)
	}
	if c.Withdrawal.Enabled && c.Withdrawal.RequireTOTP {
		add(c.Auth.TOTPSecret != "", "auth.totp_secret is required when withdrawal.require_totp is true")
	}
	add(slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level), "log.level must be debug, info, warn, or error")
	add(c.Log.Format == "json" || c.Log.Format == "console", "log.format must be json or console")

	return errors.Join(errs...)
}

func httpURL(value string) (*url.URL, error) {
	u, err := url.ParseRequestURI(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("must be an absolute http(s) URL")
	}
	return u, nil
}

// CheckReload permits only the five sections named by CFG-006 to change.
func CheckReload(current, next Config) error {
	want := current
	want.Assets = next.Assets
	want.IPN = next.IPN
	want.Resources = next.Resources
	want.Energy = next.Energy
	want.Withdrawal = next.Withdrawal
	if !reflect.DeepEqual(want, next) {
		return errors.New("SIGHUP may change only assets, ipn, resources, energy, and withdrawal (CFG-006)")
	}
	return nil
}

// DisabledConsumers returns consumers removed or disabled by a reload (CFG-006, CFG-010).
func DisabledConsumers(current, next Config) []string {
	nextEnabled := make(map[string]bool, len(next.IPN.Consumers))
	for _, consumer := range next.IPN.Consumers {
		nextEnabled[consumer.Name] = consumer.Enabled
	}
	var disabled []string
	for _, consumer := range current.IPN.Consumers {
		enabled, present := nextEnabled[consumer.Name]
		if !present || consumer.Enabled && !enabled {
			disabled = append(disabled, consumer.Name)
		}
	}
	slices.Sort(disabled)
	return disabled
}
