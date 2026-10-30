package agent

import (
	"testing"

	"github.com/cucumber/godog"
)

// ---------------------------------------------------------------------------
// BDD entry point: runs every scenario in App/features as a subtest.
// Select one scenario: go test ./internal/agent -run 'TestFeatures/<name>'
// ---------------------------------------------------------------------------

func TestFeatures(t *testing.T) {
	suiteT = t
	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"../../features"},
			Strict:   true,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("feature scenarios failed")
	}
}

// initializeScenario registers the step definitions of all features.
func initializeScenario(sc *godog.ScenarioContext) {
	w := &world{}
	registerWorldSteps(sc, w)
	registerChatTurnSteps(sc, w)
	registerInterruptSteps(sc, w)
	registerInputQueueSteps(sc, w)
	registerSessionCommandsSteps(sc, w)
	registerContextMeterSteps(sc, w)
	registerBlocksSteps(sc, w)
	registerProviderStreamSteps(sc, w)
	registerToolsSteps(sc, w)
	registerToolFlowSteps(sc, w)
	registerApprovalSteps(sc, w)
	registerAbortSteps(sc, w)
	registerBackgroundSteps(sc, w)
	registerLiveOutputSteps(sc, w)
	registerEconomySteps(sc, w)
	registerLoopGuardSteps(sc, w)
	registerRepeatSteps(sc, w)
	registerSessionSteps(sc, w)
	registerSessionNameSteps(sc, w)
	registerTraceSteps(sc, w)
	registerConfigSteps(sc, w)
	registerProviderSteps(sc, w)
	registerCompactionSteps(sc, w)
	registerModelSteps(sc, w)
	registerThinkSteps(sc, w)
	registerTaskSteps(sc, w)
	registerActivitySteps(sc, w)
	registerSkillsSteps(sc, w)
	registerMCPSteps(sc, w)
}
