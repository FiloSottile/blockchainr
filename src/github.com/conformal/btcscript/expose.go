package btcscript

import "github.com/conformal/btcwire"

// CalcScriptHash will, given the a script and hashtype for the current
// scriptmachine, calculate the doubleSha256 hash of the transaction and
// script to be used for signature signing and verification.
func CalcScriptHash(script []parsedOpcode, hashType byte, tx *btcwire.MsgTx, idx int) []byte {
	return calcScriptHash(script, hashType, tx, idx)
}

// SubScript will return the script since the last OP_CODESEPARATOR
func (s *Script) SubScript() []parsedOpcode {
	return s.subScript()
}

// RemoveOpcodeByData will return the pkscript minus any opcodes that would
// push the data in ``data'' to the stack.
func RemoveOpcodeByData(pkscript []parsedOpcode, data []byte) []parsedOpcode {
	return removeOpcodeByData(pkscript, data)
}

// Map payment types to their names.
var ScriptClassToName = scriptClassToName
