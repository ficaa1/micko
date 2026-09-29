package testkit

import "time"

// FakeClock, the injectable test clock, is defined in fake.go next to
// FakeReader.

// Compile-time assertion that FakeClock implements the app.Clock shape
// without importing internal/app (avoids an app→testkit cycle; app tests
// pass the fake where a Clock is required).
var _ interface{ Now() time.Time } = (*FakeClock)(nil)
