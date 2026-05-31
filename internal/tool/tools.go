package tool

// DefaultTools is the default set of tools available to the agent.
var DefaultTools = []Tool{
	ReadFile,
	WriteFile,
	ListFiles,
	RunPwsh,
	RunBash,
}
