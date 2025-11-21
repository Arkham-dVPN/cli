package arkham_protocol

import (
	"encoding/json"
	"fmt"
)

// This file is adapted from the tx-decoder/decoder/idl.go to be used internally by the client.

type IDL struct {
	Version      string              `json:"version"`
	Name         string              `json:"name"`
	Address      string              `json:"address"`
	Instructions []IDLInstruction    `json:"instructions"`
	Accounts     []IDLTypeDefinition `json:"accounts"`
	Events       []IDLEvent          `json:"events"`
	Types        []IDLTypeDefinition `json:"types"`
	Errors       []IDLError          `json:"errors"`
}

type IDLInstruction struct {
	Name          string       `json:"name"`
	Discriminator []byte       `json:"discriminator"`
	Args          []IDLField   `json:"args"`
	Accounts      []IDLAccount `json:"accounts"`
}

type IDLEvent struct {
	Name          string     `json:"name"`
	Discriminator []byte     `json:"discriminator"`
	Fields        []IDLField `json:"fields"`
}

type IDLField struct {
	Name string          `json:"name"`
	Type json.RawMessage `json:"type"`
}

type IDLAccount struct {
	Name     string `json:"name"`
	IsMut    bool   `json:"isMut"`
	IsSigner bool   `json:"isSigner"`
}

type IDLTypeDefinition struct {
	Name string `json:"name"`
	Type struct {
		Kind   string     `json:"kind"`
		Fields []IDLField `json:"fields"`
	} `json:"type"`
}

type IDLError struct {
	Code int    `json:"code"`
	Name string `json:"name"`
	Msg  string `json:"msg"`
}

func ParseIDL(idlBytes []byte) (*IDL, error) {
	var idl IDL
	err := json.Unmarshal(idlBytes, &idl)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling IDL JSON: %w", err)
	}
	return &idl, nil
}
