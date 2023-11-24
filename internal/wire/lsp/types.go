package lsp

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	Uri   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	Uri string `json:"uri"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Command struct {
	Title     string        `json:"title"`
	Command   string        `json:"command"`
	Arguments []interface{} `json:"arguments"`
}

type InitializeRequest struct {
	Jsonrpc string           `json:"jsonrpc"`
	Id      int              `json:"id"`
	Method  string           `json:"method"`
	Params  InitializeParams `json:"params"`
}

type InitializeParams struct {
	Capabilities ClientCapabilities `json:"capabilities"`
}

type ClientCapabilities struct {
	Workspace WorkspaceClientCapabilities `json:"workspace"`
}

type WorkspaceClientCapabilities struct {
	WorkspaceFolders bool `json:"workspaceFolders"`
}

type InitializeResponse struct {
	Jsonrpc string            `json:"jsonrpc"`
	Id      int               `json:"id"`
	Result  *InitializeResult `json:"result"`
}
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

type ServerCapabilities struct {
	TextDocumentSync int                         `json:"textDocumentSync"`
	CodeLensProvider bool                        `json:"codeLensProvider"`
	Workspace        WorkspaceServerCapabilities `json:"workspace"`
}

type WorkspaceServerCapabilities struct {
	WorkspaceFolders WorkspaceFoldersServerCapabilities `json:"workspaceFolders"`
}

type WorkspaceFoldersServerCapabilities struct {
	Supported bool `json:"supported"`
}

type ShutdownRequest struct {
	Jsonrpc string `json:"jsonrpc"`
	Id      int    `json:"id"`
}

type ShutdownResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	Id      int         `json:"id"`
	Result  interface{} `json:"result"`
}

type CodeLensRequest struct {
	Jsonrpc string         `json:"jsonrpc"`
	Id      int            `json:"id"`
	Method  string         `json:"method"`
	Params  CodeLensParams `json:"Params"`
}

type CodeLensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type CodeLensResponse struct {
	Jsonrpc string     `json:"jsonrpc"`
	Id      int        `json:"id"`
	Result  []CodeLens `json:"result"`
}

type CodeLens struct {
	Range   Range   `json:"range"`
	Command Command `json:"command"`
}

type TextDocumentNotification struct {
	Jsonrpc string             `json:"jsonrpc"`
	Method  string             `json:"method"`
	Params  TextDocumentParams `json:"params"`
}

type TextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type PublishDiagnosticsNotification struct {
	Jsonrpc string                   `json:"jsonrpc"`
	Method  string                   `json:"method"`
	Params  PublishDiagnosticsParams `json:"params"`
}

type PublishDiagnosticsParams struct {
	Uri         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Range   Range  `json:"range"`
	Message string `json:"message"`
}
