package assets

import "github.com/ethereum/go-ethereum/common"

// Asset represents a cryptocurrency asset with its properties
type Asset struct {
	Symbol   string         `json:"symbol"`
	Name     string         `json:"name"`
	Address  common.Address `json:"address"`
	Decimals int            `json:"decimals"`
}

// AssetRegistry holds all supported assets
type AssetRegistry struct {
	assets map[string]*Asset
	byAddress map[common.Address]*Asset
}

// NewAssetRegistry creates a new asset registry with all supported assets
func NewAssetRegistry() *AssetRegistry {
	registry := &AssetRegistry{
		assets:    make(map[string]*Asset),
		byAddress: make(map[common.Address]*Asset),
	}

	// Define all supported assets
	supportedAssets := []*Asset{
		{
			Symbol:   "LBTC",
			Name:     "Lombard Staked BTC",
			Address:  common.HexToAddress("0x8236a87084f8b84306f72007f36f2618a5634494"),
			Decimals: 8,
		},
		{
			Symbol:   "WBTC",
			Name:     "Wrapped BTC",
			Address:  common.HexToAddress("0x2260fac5e5542a773aa44fbcfedf7c193bc2c599"),
			Decimals: 8,
		},
		{
			Symbol:   "CBTC",
			Name:     "Coinbase Wrapped BTC",
			Address:  common.HexToAddress("0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf"),
			Decimals: 8,
		},
		{
			Symbol:   "LBTCv",
			Name:     "Lombard BTC Vault",
			Address:  common.HexToAddress("0x5401b8620E5FB570064CA9114fd1e135fd77D57c"),
			Decimals: 8,
		},
	}

	// Register all assets
	for _, asset := range supportedAssets {
		registry.assets[asset.Symbol] = asset
		registry.byAddress[asset.Address] = asset
	}

	return registry
}

// GetBySymbol returns an asset by its symbol (case-insensitive)
func (r *AssetRegistry) GetBySymbol(symbol string) (*Asset, bool) {
	// Try exact match first
	if asset, exists := r.assets[symbol]; exists {
		return asset, true
	}
	
	// Try case-insensitive match
	for _, asset := range r.assets {
		if asset.Symbol == symbol {
			return asset, true
		}
	}
	
	return nil, false
}

// GetByAddress returns an asset by its contract address
func (r *AssetRegistry) GetByAddress(address common.Address) (*Asset, bool) {
	asset, exists := r.byAddress[address]
	return asset, exists
}

// GetAll returns all registered assets
func (r *AssetRegistry) GetAll() map[string]*Asset {
	result := make(map[string]*Asset)
	for symbol, asset := range r.assets {
		result[symbol] = asset
	}
	return result
}

// GetAllAsArray returns all assets as an array
func (r *AssetRegistry) GetAllAsArray() []*Asset {
	assets := make([]*Asset, 0, len(r.assets))
	for _, asset := range r.assets {
		assets = append(assets, asset)
	}
	return assets
}

// IsSupported checks if a symbol is supported
func (r *AssetRegistry) IsSupported(symbol string) bool {
	_, exists := r.GetBySymbol(symbol)
	return exists
}

// GetSupportedSymbols returns all supported asset symbols
func (r *AssetRegistry) GetSupportedSymbols() []string {
	symbols := make([]string, 0, len(r.assets))
	for symbol := range r.assets {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// Global asset registry instance
var GlobalRegistry = NewAssetRegistry()

// Contract addresses for convenience
var (
	LBTCAddress  = GlobalRegistry.assets["LBTC"].Address
	WBTCAddress  = GlobalRegistry.assets["WBTC"].Address
	CBTCAddress  = GlobalRegistry.assets["CBTC"].Address
	LBTCVAddress = GlobalRegistry.assets["LBTCv"].Address
)

// Contract addresses for other components
const (
	TellerContractAddress        = "0x4e8f5128f473c6948127f9cbca474a6700f99bab"
	AtomicRequestContractAddress = "0x3b4aCd8879fb60586cCd74bC2F831A4C5E7DbBf8"
	AccountantContractAddress    = "0x28634D0c5edC67CF2450E74deA49B90a4FF93dCE"
)