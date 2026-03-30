package session

import (
	"testing"

	"github.com/wisbric/agentic-hive/internal/testutil"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, testutil.GoleakOptions()...)
}
