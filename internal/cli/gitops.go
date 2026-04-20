package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/taskrun"
)

func runApply(args []string) int {
	patchPath := ""
	checkOnly := false

	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--file":
			value, next, err := takeFlagValue(args, index, "--file")
			if err != nil {
				fmt.Fprintf(os.Stderr, "apply: %v\n", err)
				return 2
			}
			patchPath = value
			index = next
		case "--check":
			checkOnly = true
		default:
			fmt.Fprintf(os.Stderr, "apply: unknown flag %q\n", args[index])
			return 2
		}
	}

	patchData, err := readPatchInput(patchPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "apply: %v\n", err)
		return 1
	}
	if len(bytes.TrimSpace(patchData)) == 0 {
		fmt.Fprintln(os.Stderr, "apply: patch input is empty")
		return 2
	}

	commandArgs := []string{"apply", "--whitespace=nowarn"}
	if checkOnly {
		commandArgs = append(commandArgs, "--check")
	}
	commandArgs = append(commandArgs, "-")

	cmd := exec.Command("git", commandArgs...)
	cmd.Stdin = bytes.NewReader(patchData)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			fmt.Fprintln(os.Stderr, strings.TrimSpace(string(output)))
		}
		fmt.Fprintf(os.Stderr, "apply: %v\n", err)
		return 1
	}

	if checkOnly {
		fmt.Println("patch is valid")
	} else {
		fmt.Println("patch applied")
	}
	return 0
}

func runReview(args []string) int {
	diffCommand := []string{"git", "diff", "--"}
	runOptions := taskrun.Options{}

	for index := 0; index < len(args); index++ {
		if next, handled, err := consumeToolPolicyFlag(&runOptions, args, index); handled {
			if err != nil {
				fmt.Fprintf(os.Stderr, "review: %v\n", err)
				return 2
			}
			index = next
			continue
		}
		switch args[index] {
		case "--json":
			runOptions.JSON = true
		case "--no-stream":
			runOptions.DisableStreaming = true
		case "--cached", "--staged":
			diffCommand = []string{"git", "diff", "--cached", "--"}
		case "--base":
			value, next, err := takeFlagValue(args, index, "--base")
			if err != nil {
				fmt.Fprintf(os.Stderr, "review: %v\n", err)
				return 2
			}
			diffCommand = []string{"git", "diff", value + "...HEAD", "--"}
			index = next
		case "--commit":
			value, next, err := takeFlagValue(args, index, "--commit")
			if err != nil {
				fmt.Fprintf(os.Stderr, "review: %v\n", err)
				return 2
			}
			diffCommand = []string{"git", "show", "--stat=0", "--format=medium", value, "--"}
			index = next
		case "--model":
			value, next, err := takeFlagValue(args, index, "--model")
			if err != nil {
				fmt.Fprintf(os.Stderr, "review: %v\n", err)
				return 2
			}
			runOptions.Model = value
			index = next
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				fmt.Fprintf(os.Stderr, "review: %v\n", err)
				return 2
			}
			runOptions.Profile = value
			index = next
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				fmt.Fprintf(os.Stderr, "review: %v\n", err)
				return 2
			}
			runOptions.Provider = value
			index = next
		case "--reasoning":
			value, next, err := takeFlagValue(args, index, "--reasoning")
			if err != nil {
				fmt.Fprintf(os.Stderr, "review: %v\n", err)
				return 2
			}
			runOptions.ReasoningEffort = value
			index = next
		default:
			fmt.Fprintf(os.Stderr, "review: unknown flag %q\n", args[index])
			return 2
		}
	}

	diff, err := exec.Command(diffCommand[0], diffCommand[1:]...).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "review: %v\n", err)
		if len(diff) > 0 {
			fmt.Fprintln(os.Stderr, strings.TrimSpace(string(diff)))
		}
		return 1
	}
	if strings.TrimSpace(string(diff)) == "" {
		fmt.Println("no diff to review")
		return 0
	}

	runOptions.Prompt = buildReviewPrompt(string(diff))
	result, err := taskrun.Run(contextBackground(), runOptions)
	if err != nil {
		fmt.Fprintf(os.Stderr, "review: %v\n", err)
		return 1
	}

	if entry, err := persistNewSession(result); err == nil {
		result.SessionPath = entry.Path
	} else {
		fmt.Fprintf(os.Stderr, "review: warning: failed to save session: %v\n", err)
	}

	if runOptions.JSON {
		return printJSON(result)
	}
	if err := taskrun.Print(result); err != nil {
		fmt.Fprintf(os.Stderr, "review: %v\n", err)
		return 1
	}
	return 0
}

func readPatchInput(path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		return os.ReadFile(path)
	}
	return io.ReadAll(os.Stdin)
}

func buildReviewPrompt(diff string) string {
	return strings.TrimSpace(`Review the following changes like a rigorous code reviewer.

Prioritize:
- bugs
- behavioral regressions
- security or data-loss risk
- missing tests
- weak assumptions

Return findings first with concrete evidence.
Keep the summary brief.

Diff:

` + diff)
}
