package sandbox

// VerifyResult is the outcome of running tests/linting inside the sandbox.
type VerifyResult struct {
	Passed   bool
	Output   string
	Feedback string
}

// DetectVerifyCommands returns shell commands to run tests and linting based
// on which project files exist. The caller is expected to check file existence
// inside the sandbox via ExecCollect before calling this.
func DetectVerifyCommands(existingFiles map[string]bool) []string {
	var cmds []string

	switch {
	case existingFiles["go.mod"]:
		cmds = append(cmds, "go test ./... 2>&1")
	case existingFiles["package.json"]:
		cmds = append(cmds, "npm test --if-present 2>&1")
	case existingFiles["Cargo.toml"]:
		cmds = append(cmds, "cargo test 2>&1")
	case existingFiles["requirements.txt"] || existingFiles["pyproject.toml"] || existingFiles["setup.py"]:
		cmds = append(cmds, "python -m pytest 2>&1 || python -m unittest discover 2>&1")
	case existingFiles["Makefile"]:
		cmds = append(cmds, "make test 2>&1")
	}

	switch {
	case existingFiles["go.mod"]:
		cmds = append(cmds, "go vet ./... 2>&1")
	case existingFiles[".eslintrc.js"] || existingFiles[".eslintrc.json"] || existingFiles["eslint.config.js"] || existingFiles["eslint.config.mjs"]:
		cmds = append(cmds, "npx eslint . 2>&1")
	}

	return cmds
}
