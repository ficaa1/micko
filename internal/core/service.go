package core

// Service-level seams that only depend on core types. The REST adapter (internal/argo) is
// the production implementation; internal/testkit provides the fake.
//
// This file intentionally carries no logic: the Reader interface (types.go)
// is the frozen contract; nothing here may widen it without a deliberate
// contract change.
