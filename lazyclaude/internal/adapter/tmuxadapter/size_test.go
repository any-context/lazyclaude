package tmuxadapter_test

import (
	"testing"

	"github.com/KEMSHlM/lazyclaude/internal/adapter/tmuxadapter"
	"github.com/stretchr/testify/assert"
)

func TestEstimatePopupSize_Bash(t *testing.T) {
	t.Parallel()
	w, h := tmuxadapter.EstimatePopupSize("Bash", `{"command":"ls -la"}`, 200, 50)
	assert.Greater(t, w, 30)
	assert.Greater(t, h, 20)
	assert.LessOrEqual(t, w, 90)
	assert.LessOrEqual(t, h, 90)
}

func TestEstimatePopupSize_Write(t *testing.T) {
	t.Parallel()
	w, h := tmuxadapter.EstimatePopupSize("Write", `{"file_path":"/tmp/test.txt","content":"hello world"}`, 200, 50)
	assert.Greater(t, w, 40)
	assert.Greater(t, h, 20)
}

func TestEstimatePopupSize_LongInput(t *testing.T) {
	t.Parallel()
	longInput := `{"command":"` + string(make([]byte, 5000)) + `"}`
	w, h := tmuxadapter.EstimatePopupSize("Bash", longInput, 200, 50)
	// Should cap at reasonable maximums
	assert.LessOrEqual(t, w, 90)
	assert.LessOrEqual(t, h, 90)
}

func TestEstimatePopupSize_SmallTerminal(t *testing.T) {
	t.Parallel()
	w, h := tmuxadapter.EstimatePopupSize("Bash", `{"command":"ls"}`, 40, 15)
	assert.Greater(t, w, 0)
	assert.Greater(t, h, 0)
}
