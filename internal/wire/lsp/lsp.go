package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func ReadBuffer(reader *bufio.Reader) ([]byte, bool) {
	var length int
	for {
		header, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading header: %v\n", err)
			return nil, false
		}
		if header == "\r\n" {
			break
		}
		// TODO: trim the remaining \r also
		header = strings.TrimSpace(header)
		switch {
		case strings.HasPrefix(header, "Content-Length: "):
			value := strings.TrimPrefix(header, "Content-Length: ")
			length, err = strconv.Atoi(value)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Content-Length is not a valid integer: %v\n", err)
				return nil, false
			}
		case strings.HasPrefix(header, "Content-Type: "):
			value := strings.TrimPrefix(header, "Content-Type: ")
			if value != "application/vscode-jsonrpc; charset=utf-8" {
				fmt.Fprintf(os.Stderr, "Content-Type is invalid: %v\n", value)
				return nil, false
			}
		default:
			fmt.Fprintf(os.Stderr, "header field name is invalid: %v", header)
			return nil, false
		}
	}
	// Read len bytes of content
	buf := make([]byte, length)
	n, err := io.ReadFull(reader, buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading content %v\n", err)
		return nil, false
	}
	if n != length {
		fmt.Fprintln(os.Stderr, "Content-Length and content length do not match")
		return nil, false
	}
	return buf, true
}

func ParseMessage(buf []byte) (map[string]interface{}, bool) {
	in := make(map[string]interface{})
	if err := json.Unmarshal(buf, &in); err != nil {
		fmt.Fprintf(os.Stderr, "error deserializing request: %v\n", err)
		return nil, false
	}
	return in, true
}

func ParseRequest(buf []byte, req interface{}) bool {
	if err := json.Unmarshal(buf, req); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing request: %v\n", err)
		return false
	}
	return true
}

func StringifyResponse(res interface{}) bool {
	bytes, err := json.Marshal(res)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error serializing response: %v\n", err)
		return false
	}
	fmt.Printf("Content-Length: %d\r\n", len(bytes))
	fmt.Printf("\r\n")
	// TODO: \r\n at the end is redundant
	fmt.Print(string(bytes) + "\r\n")
	return true
}

// func calculatePos(fset *token.FileSet, path string, line int, char int) token.Pos {
// 	var file *token.File
// 	fset.Iterate(func(f *token.File) bool {
// 		if f.Name() == path {
// 			file = f
// 			return false
// 		}
// 		return true
// 	})
// 	offset := int(file.LineStart(line)) - int(file.Base())
// 	offset += char
// 	return file.Pos(offset)
// }
