package lsp

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type TextDocumentIdentifier struct {
	Uri string `json:"uri"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
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
	Configuration    bool `json:"configuration"`
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
	HoverProvider    bool                        `json:"hoverProvider"`
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

type HoverRequest struct {
	Jsonrpc string      `json:"jsonrpc"`
	Id      int         `json:"id"`
	Method  string      `json:"method"`
	Params  HoverParams `json:"Params"`
}

type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type HoverResponse struct {
	Jsonrpc string       `json:"jsonrpc"`
	Id      int          `json:"id"`
	Result  *HoverResult `json:"result"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
}

type DidSaveTextDocumentNotification struct {
	Jsonrpc string                    `json:"jsonrpc"`
	Method  string                    `json:"method"`
	Params  DidSaveTextDocumentParams `json:"params"`
}

type DidSaveTextDocumentParams struct {
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
