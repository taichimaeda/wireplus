package lsp

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
	Jsonrpc string           `json:"jsonrpc"`
	Id      int              `json:"id"`
	Result  InitializeResult `json:"result"`
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

type TextDocumentIdentifier struct {
	Uri string `json:"uri"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type HoverResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	Id      int         `json:"id"`
	Result  HoverResult `json:"result"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
