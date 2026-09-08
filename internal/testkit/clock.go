package testkit

import "time"

// FakeClock is the canonical injectable clock (plan §8 F1 slice 3: fake
// clock); it lives here so the testkit file list matches the plan card.
// The implementation is in fake.go next to FakeReader.

// Compile-time assertion that FakeClock implements the app.Clock shape
// without importing internal/app (avoids an app→testkit cycle; app tests
// pass the fake where a Clock is required).
var _ interface{ Now() time.Time } = (*FakeClock)(nil)
