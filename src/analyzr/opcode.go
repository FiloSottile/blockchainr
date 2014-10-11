package main

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/conformal/btcdb"
	"github.com/conformal/btcec"
	"github.com/conformal/btcnet"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
)

func processPubKeyHash(db btcdb.Db, rd *rData) error {
	sigScript := rd.txIn.SignatureScript
	pkScript := rd.txPrevOut.PkScript
	script, err := btcscript.NewScript(sigScript, pkScript, rd.txInIndex, rd.tx.MsgTx(), 0)
	if err != nil {
		return fmt.Errorf("failed btcscript.NewScript - h %v: %v\n", rd.in.H, err)
	}

	for script.Next() != btcscript.OP_CHECKSIG {
		_, err := script.Step()
		if err != nil {
			return fmt.Errorf("Failed Step - in %v: %v\n", rd.in, err)
		}
	}

	data := script.GetStack()

	rd.sigStr = data[0]
	rd.pkStr = data[1]

	aPubKey, err := btcutil.NewAddressPubKey(rd.pkStr, &btcnet.MainNetParams)
	if err != nil {
		return fmt.Errorf("Pubkey parse error: %v", err)
	}
	rd.address = aPubKey.EncodeAddress()
	rd.compressed = aPubKey.Format() == btcutil.PKFCompressed

	// From github.com/conformal/btcscript/opcode.go

	// Signature actually needs needs to be longer than this, but we need
	// at least  1 byte for the below. btcec will check full length upon
	// parsing the signature.
	if len(rd.sigStr) < 1 {
		return fmt.Errorf("OP_CHECKSIG ERROR")
	}

	// Trim off hashtype from the signature string.
	hashType := rd.sigStr[len(rd.sigStr)-1]
	sigStr := rd.sigStr[:len(rd.sigStr)-1]

	// Get script from the last OP_CODESEPARATOR and without any subsequent
	// OP_CODESEPARATORs
	subScript := script.SubScript()

	// Unlikely to hit any cases here, but remove the signature from
	// the script if present.
	subScript = btcscript.RemoveOpcodeByData(subScript, sigStr)

	hash := btcscript.CalcScriptHash(subScript, hashType, rd.tx.MsgTx(), rd.txInIndex)

	pubKey, err := btcec.ParsePubKey(rd.pkStr, btcec.S256())
	if err != nil {
		return fmt.Errorf("OP_CHECKSIG ERROR")
	}

	signature, err := btcec.ParseSignature(sigStr, btcec.S256())
	if err != nil {
		return fmt.Errorf("OP_CHECKSIG ERROR")
	}

	// log.Printf("op_checksig\n"+
	//  "pubKey:\n%v"+
	//  "pubKey.X: %v\n"+
	//  "pubKey.Y: %v\n"+
	//  "signature.R: %v\n"+
	//  "signature.S: %v\n"+
	//  "checkScriptHash:\n%v",
	//  hex.Dump(pkStr), pubKey.X, pubKey.Y,
	//  signature.R, signature.S, hex.Dump(hash))

	if ok := ecdsa.Verify(pubKey.ToECDSA(), hash, signature.R, signature.S); !ok {
		return fmt.Errorf("OP_CHECKSIG FAIL")
	}

	rd.signature = signature
	rd.pubKey = pubKey
	rd.hash = hash

	return nil
}
