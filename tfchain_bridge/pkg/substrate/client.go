package substrate

import (
	"fmt"

	"github.com/threefoldtech/substrate-client"
)

var (
	//ErrInvalidVersion is returned if version 4bytes is invalid
	ErrInvalidVersion = fmt.Errorf("invalid version")
	//ErrUnknownVersion is returned if version number is not supported
	ErrUnknownVersion = fmt.Errorf("unknown version")
	//ErrNotFound is returned if an object is not found
	ErrNotFound = fmt.Errorf("object not found")
)

// Versioned base for all types
type Versioned struct {
	Version uint32
}

type SubstrateClient struct {
	*substrate.Substrate
}

// NewSubstrate creates a substrate client
func NewSubstrateClient(url string) (*SubstrateClient, error) {
	cl, err := substrate.NewSubstrate(url)
	if err != nil {
		return nil, err
	}

	return &SubstrateClient{
		cl,
	}, nil
}
