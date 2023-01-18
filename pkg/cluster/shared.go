package cluster

import (
	internalproviders "sigs.k8s.io/kind/pkg/cluster/shared/providers"
	"sigs.k8s.io/kind/pkg/log"
)

// ProviderWrap wrap the struct Provider, due to struct has lower-case not-export fields
// so that we can use Provider and Logger field
type ProviderWrapper struct {
	Provider internalproviders.Provider
	Logger   log.Logger
}

func NewProviderWrapper(options ...ProviderOption) *ProviderWrapper {
	p := NewProvider(options...)
	return &ProviderWrapper{
		Provider: p.provider,
		Logger:   p.logger,
	}
}
