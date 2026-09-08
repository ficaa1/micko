package core

// Service-level seams that only depend on core types. A1's REST adapter is
// the production implementation; internal/testkit provides the fake.
//
// This file intentionally carries no logic: the Reader interface (types.go)
// is the frozen contract; nothing here may widen it without an F-gated
// contract amendment (plan §4).
