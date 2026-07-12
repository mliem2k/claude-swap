package cswap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// confirmPrompt returns a confirm func(string) bool matching the shape
// every engine confirmation callback expects (AddAccount, RemoveAccount,
// Purge), reading a real y/N answer from in and echoing the prompt
// (question text plus a "[y/N] " suffix, matching Python's
// f"{question} [y/N] " input() prompts) to out. Any answer other than a
// bare "y"/"Y" declines, matching Python's `confirm.lower() != "y"` check.
func confirmPrompt(in io.Reader, out io.Writer) func(string) bool {
	reader := bufio.NewReader(in)
	return func(question string) bool {
		fmt.Fprintf(out, "%s [y/N] ", question)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		return answer == "y"
	}
}

// disambiguatePrompt returns a disambiguate func(candidates []string) string
// matching SwitchTo/RemoveAccount's shape: prints Python's exact
// interactive disambiguation prompt (a header naming identifier, one
// numbered line per candidate with its display tag, then promptText
// reading a bare account number from in), returning the chosen number or
// "" (cancel, printed as a dimmed "Cancelled" line) for any
// blank/non-matching answer.
func disambiguatePrompt(s *ClaudeAccountSwitcher, identifier, promptText string, in io.Reader, out io.Writer) func(candidates []string) string {
	reader := bufio.NewReader(in)
	return func(candidates []string) string {
		fmt.Fprintf(out, "Multiple accounts found for '%s':\n", identifier)
		data := s.GetSequenceData()
		for _, num := range candidates {
			tag := ""
			if data != nil {
				if acc, ok := data.Accounts[num]; ok {
					tag = GetDisplayTag(acc.Email, acc.OrganizationName, acc.OrganizationUUID)
				}
			}
			fmt.Fprintf(out, "  %s: %s %s\n", num, identifier, Muted(fmt.Sprintf("[%s]", tag)))
		}
		fmt.Fprint(out, promptText)
		line, _ := reader.ReadString('\n')
		choice := strings.TrimSpace(line)
		for _, num := range candidates {
			if choice == num {
				return choice
			}
		}
		fmt.Fprintln(out, Dimmed("Cancelled"))
		return ""
	}
}

// printJSON mirrors `print(json.dumps(payload, indent=2))`.
func printJSON(out io.Writer, payload map[string]any) {
	data, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Fprintln(out, string(data))
}

// printLines writes one line per entry, matching how ListAccountsResult/
// StatusResult/etc. carry pre-styled human-display output as a slice
// instead of printing it themselves (this port's established convention).
func printLines(out io.Writer, lines []string) {
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
}
