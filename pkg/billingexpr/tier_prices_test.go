package billingexpr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractTierUnitPrices(t *testing.T) {
	expr := `len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6) : tier("long", p * 6 + c * 22.5 + cr * 0.6)`

	standard, ok := ExtractTierUnitPrices(expr, "standard")
	require.True(t, ok)
	require.Equal(t, 3.0, standard.Input)
	require.Equal(t, 15.0, standard.Output)
	require.Equal(t, 0.3, standard.CacheRead)
	require.Equal(t, 3.75, standard.CacheWrite)
	require.Equal(t, 6.0, standard.CacheWrite1h)
	require.True(t, standard.UsedVars["cc1h"])

	long, ok := ExtractTierUnitPrices(expr, "long")
	require.True(t, ok)
	require.Equal(t, 6.0, long.Input)
	require.Equal(t, 22.5, long.Output)
	require.Equal(t, 0.6, long.CacheRead)
	require.False(t, long.UsedVars["cc"])
}

func TestExtractTierUnitPricesRejectsCustomOrAmbiguousRules(t *testing.T) {
	_, ok := ExtractTierUnitPrices(`tier("base", p * 2 + img * 5)`, "base")
	require.False(t, ok)

	_, ok = ExtractTierUnitPrices(`tier("base", p * 2 + c * 10) * 0.5`, "base")
	require.False(t, ok)

	_, ok = ExtractTierUnitPrices(`p > 1 ? tier("same", p * 2 + c * 10) : tier("same", p * 3 + c * 12)`, "same")
	require.False(t, ok)
}

func TestExtractTierUnitPricesAllowsConstantDifference(t *testing.T) {
	prices, ok := ExtractTierUnitPrices(`tier("base", p * 2 + c * 8 + 1.25)`, "base")
	require.True(t, ok)
	require.Equal(t, 2.0, prices.Input)
	require.Equal(t, 8.0, prices.Output)
}
