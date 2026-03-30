package testutil

import "go.uber.org/goleak"

// GoleakOptions returns the shared set of goleak options that filter known
// long-lived goroutines. Use in every package's TestMain:
//
//	goleak.VerifyTestMain(m, testutil.GoleakOptions()...)
func GoleakOptions() []goleak.Option {
	return []goleak.Option{
		goleak.IgnoreTopFunction("go.opencensus.io/stats/view.(*worker).start"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
		// net/http background goroutines from httptest.Server
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
	}
}
