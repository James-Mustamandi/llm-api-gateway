package provider

type Registry struct {
	providers []Provider
}

func NewRegistry(providers ...Provider) *Registry {
	return &Registry{providers: providers}
}

func (registry *Registry) Providers() []Provider {
	return registry.providers
}

func (registry *Registry) Primary() Provider {
	if len(registry.providers) == 0 {
		return nil
	}
	return registry.providers[0]
}