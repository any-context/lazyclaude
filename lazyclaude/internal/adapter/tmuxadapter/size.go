package tmuxadapter

// EstimatePopupSize returns width and height percentages for a popup
// based on tool name and input length.
func EstimatePopupSize(toolName, toolInput string, termW, termH int) (wPct, hPct int) {
	inputLen := len(toolInput)

	switch {
	case toolName == "Bash" || toolName == "bash":
		wPct = 60
		hPct = 40
	case toolName == "Read" || toolName == "Glob" || toolName == "Grep":
		wPct = 50
		hPct = 30
	case toolName == "Write" || toolName == "Edit":
		wPct = 70
		hPct = 60
	case toolName == "Agent":
		wPct = 60
		hPct = 50
	default:
		wPct = 55
		hPct = 35
	}

	if inputLen > 200 {
		wPct += 10
	}
	if inputLen > 500 {
		hPct += 10
	}
	if inputLen > 1000 {
		wPct += 5
		hPct += 10
	}

	if wPct > 90 {
		wPct = 90
	}
	if hPct > 90 {
		hPct = 90
	}
	if wPct < 30 {
		wPct = 30
	}
	if hPct < 20 {
		hPct = 20
	}
	if termW < 80 {
		wPct = 90
	}
	if termH < 24 {
		hPct = 90
	}

	return wPct, hPct
}
