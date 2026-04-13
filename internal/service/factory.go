package service

import (
	"encoding/json"
	"fmt"
	"sort"
)

// DestinationFactory creates a [Destination] from a config map.
// Factories are registered in main via [RegisterDestinationFactory] to
// avoid circular imports between the service and destinations packages.
type DestinationFactory func(config map[string]string) Destination

var destinationFactories = make(map[string]DestinationFactory)

// RegisterDestinationFactory registers a factory for the given destination
// type string (e.g., "remarkable", "file").
func RegisterDestinationFactory(typeStr string, factory DestinationFactory) {
	destinationFactories[typeStr] = factory
}

// GetDestinationFactory returns the factory for typeStr and whether it exists.
func GetDestinationFactory(typeStr string) (DestinationFactory, bool) {
	f, ok := destinationFactories[typeStr]
	return f, ok
}

// RegisteredDestinationTypes returns a sorted list of registered destination type names.
func RegisteredDestinationTypes() []string {
	types := make([]string, 0, len(destinationFactories))
	for t := range destinationFactories {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// CreateDestinationInstance uses the registered factory to create a [Destination] of the given type.
func CreateDestinationInstance(typeStr, configJSON string) (Destination, error) {
	factory, ok := destinationFactories[typeStr]
	if !ok {
		return nil, fmt.Errorf("unknown destination type: %s", typeStr)
	}

	var config map[string]string
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return factory(config), nil
}
