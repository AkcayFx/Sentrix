package graph

import (
	"fmt"
	"io"

	"github.com/99designs/gqlgen/graphql"
	"github.com/google/uuid"
)

// UUID is a custom scalar type wrapping uuid.UUID for GraphQL.
type UUID = uuid.UUID

// MarshalUUID implements the graphql.Marshaler interface for UUID.
func MarshalUUID(u uuid.UUID) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, `"`+u.String()+`"`)
	})
}

// UnmarshalUUID implements the graphql.Unmarshaler interface for UUID.
func UnmarshalUUID(v interface{}) (uuid.UUID, error) {
	switch val := v.(type) {
	case string:
		return uuid.Parse(val)
	default:
		return uuid.Nil, fmt.Errorf("uuid must be a string, got %T", v)
	}
}
