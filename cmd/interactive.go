package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/dstockto/fil/api"
	"github.com/dstockto/fil/models"
	"github.com/manifoldco/promptui"
	"github.com/mattn/go-isatty"
)

// isInteractiveAllowed returns true when the user did not disable interaction
// via flag and when the process is attached to a TTY suitable for prompting.
func isInteractiveAllowed(nonInteractive bool) bool {
	if nonInteractive {
		return false
	}
	// Require stdin, stdout, and stderr to be terminals and TERM to be sane
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) || !isatty.IsTerminal(os.Stderr.Fd()) {
		return false
	}
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if term == "" || term == "dumb" {
		return false
	}
	return true
}

// selectSpoolInteractively shows a selectable list of spools and returns the
// chosen spool. If the user cancels the prompt (Esc or Ctrl+C), canceled is true.
// initialTerm is the user's original query. If initialCandidates is non-empty,
// they will be shown first in the list, followed by other spools in scope.
func selectSpoolInteractively(apiClient *api.Client, initialTerm string, query map[string]string, initialCandidates []models.FindSpool, forceSimple bool) (models.FindSpool, bool, error) {
	// Load all spools within scope to support full search, but order so that
	// initialCandidates (ambiguous matches) appear first.
	all, err := apiClient.FindSpoolsByName("*", nil, query)
	if err != nil {
		return models.FindSpool{}, false, err
	}
	if len(all) == 0 {
		return models.FindSpool{}, false, fmt.Errorf("no spools available to select from")
	}

	// Build ordered candidates: initial first (unique), then the rest
	seen := map[int]struct{}{}
	candidates := make([]models.FindSpool, 0, len(all))
	for _, s := range initialCandidates {
		if _, ok := seen[s.Id]; !ok {
			candidates = append(candidates, s)
			seen[s.Id] = struct{}{}
		}
	}
	for _, s := range all {
		if _, ok := seen[s.Id]; !ok {
			candidates = append(candidates, s)
			seen[s.Id] = struct{}{}
		}
	}

	// If advanced TUI is not appropriate, fall back to simple selector
	// When user forces --simple-select, limit choices to the initial ambiguous matches only.
	if forceSimple {
		// Build a unique list only from the initial candidates
		initOnly := make([]models.FindSpool, 0, len(initialCandidates))
		seenInit := map[int]struct{}{}
		for _, s := range initialCandidates {
			if _, ok := seenInit[s.Id]; !ok {
				initOnly = append(initOnly, s)
				seenInit[s.Id] = struct{}{}
			}
		}
		if len(initOnly) == 0 {
			return models.FindSpool{}, true, fmt.Errorf("no spools matched the original search for simple selector")
		}
		return selectSpoolSimple(initOnly, initialTerm)
	}
	if !supportsAdvancedTUI() {
		return selectSpoolSimple(candidates, initialTerm)
	}

	// Prepare string items without ANSI for stability
	items := make([]string, len(candidates))
	for i, it := range candidates {
		items[i] = it.String()
	}

	searcher := func(input string, index int) bool {
		item := candidates[index]
		needle := strings.ToLower(strings.TrimSpace(input))
		if needle == "" {
			return true
		}
		fields := []string{
			fmt.Sprintf("%d", item.Id),
			item.Filament.Vendor.Name,
			item.Filament.Name,
			item.Location,
			item.Filament.Material,
			item.Filament.ColorHex,
		}
		joined := strings.ToLower(strings.Join(fields, " "))
		return strings.Contains(joined, needle)
	}

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "▸ {{ . | cyan }}",
		Inactive: "  {{ . }}",
		Selected: "✔ {{ . | green }}",
	}

	label := "Select the intended spool (type to filter; Esc to cancel)"
	if strings.TrimSpace(initialTerm) != "" {
		label = fmt.Sprintf("Select the intended spool for '%s' (type to filter; Esc to cancel)", initialTerm)
	}

	prompt := promptui.Select{
		Label:             label,
		Items:             items,
		Templates:         templates,
		Size:              12,
		Searcher:          searcher,
		StartInSearchMode: true,
		Stdin:             os.Stdin,
		Stdout:            NoBellStdout,
	}

	idx, _, perr := prompt.Run()
	if perr != nil {
		if perr == promptui.ErrInterrupt || perr == promptui.ErrAbort {
			return models.FindSpool{}, true, nil
		}
		// Fall back to simple selector on unexpected prompt errors
		return selectSpoolSimple(candidates, initialTerm)
	}

	return candidates[idx], false, nil
}

// supportsAdvancedTUI gates the promptui-based UI to terminals that typically
// support full-screen cursor movement without glitches.
func supportsAdvancedTUI() bool {
	// On macOS/iTerm2 and most terminals xterm-256color is fine; keep this simple.
	// We already checked TERM in isInteractiveAllowed. Here we can add more guards
	// if needed later.
	return true
}

// selectSpoolSimple provides a numbered list over basic stdin without cursor
// control. User types a number or presses Enter to cancel.
func selectSpoolSimple(candidates []models.FindSpool, initialTerm string) (models.FindSpool, bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Multiple spools match; please choose one:")
	if strings.TrimSpace(initialTerm) != "" {
		fmt.Printf("(for '%s')\n", initialTerm)
	}
	for i, s := range candidates {
		fmt.Printf("%2d) %s\n", i+1, s.String())
	}
	fmt.Print("Enter number to select, or press Enter to cancel: ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return models.FindSpool{}, true, nil
	}
	// Allow matching by ID as well
	for idx := range candidates {
		if line == fmt.Sprintf("%d", idx+1) {
			return candidates[idx], false, nil
		}
	}
	// Try direct spool ID entry
	for _, s := range candidates {
		if line == fmt.Sprintf("%d", s.Id) {
			return s, false, nil
		}
	}
	return models.FindSpool{}, true, fmt.Errorf("invalid selection: %q", line)
}
