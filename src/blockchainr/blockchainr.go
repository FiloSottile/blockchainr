// Copyright (c) 2013 Conformal Systems LLC.
// Copyright (c) 2014 Filippo Valsorda
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io"

	"github.com/davecgh/go-spew/spew"

	"github.com/conformal/btcchain"
	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/ldb"
	"github.com/conformal/btcec"
	"github.com/conformal/btclog"
	"github.com/conformal/btcutil"
)

// TODO: monkey-patch OP_CHECKSIG

func PopData(SignatureScript []byte) ([]byte, []byte, error) {
	if len(SignatureScript) < 1 {
		return nil, nil, fmt.Errorf("empty SignatureScript")
	}
	opcode := SignatureScript[0]

	if opcode >= 1 && opcode <= 75 {
		if len(SignatureScript) < int(opcode+1) {
			return nil, nil, fmt.Errorf("SignatureScript too short")
		}
		sigStr := SignatureScript[1 : opcode+1]
		remaining := SignatureScript[opcode+1:]
		return sigStr, remaining, nil
	}

	// TODO: OP_PUSHDATA1 OP_PUSHDATA2 OP_PUSHDATA3
	if opcode >= 76 && opcode <= 78 {
		return nil, nil, fmt.Errorf("FIXME: OP_PUSHDATA %v", opcode)
	}

	return nil, nil, fmt.Errorf("the first opcode (%x) is not a data push", opcode)
}

func dumpBlock(height int64, db btcdb.Db,
	duplicates map[string][]string, rValuesMap map[string]string,
	errorFile io.Writer, log btclog.Logger) (int, error) {
	sigCounter := 0

	sha, err := db.FetchBlockShaByHeight(height)
	if err != nil {
		return 0, err
	}
	blk, err := db.FetchBlockBySha(sha)
	if err != nil {
		return 0, err
	}
	// rblk, err := blk.Bytes()
	// if err != nil {
	// 	return 0, err
	// }
	blkid := blk.Height()
	if blkid != height {
		return 0, fmt.Errorf("WHAT!?")
	}

	// log.Debugf("Block %v depth %v", sha, blkid)

	// log.Debugf("Block %v depth %v %v", sha, blkid, spew.Sdump(rblk))
	mblk := blk.MsgBlock()
	// log.Debugf("Block %v depth %v %v", sha, blkid, spew.Sdump(mblk))

	// log.Debugf("Num transactions %v", len(mblk.Transactions))
	for i, tx := range mblk.Transactions {

		txsha, err := tx.TxSha()
		if err != nil {
			log.Warnf("Block %v (%v)", blkid, sha)
			log.Warnf("tx %v (%v)", i, &txsha)
			log.Warnf("Error: %v", err)
			continue
		}

		// log.Debugf("tx %v: %v", i, &txsha)

		if btcchain.IsCoinBase(btcutil.NewTx(tx)) {
			// log.Debugf("tx %v: skipping (coinbase)", i)
			continue
		}

		for t, txin := range tx.TxIn {
			// log.Debugf("tx %v: TxIn %v: SignatureScript: %v", i, t, spew.Sdump(txin.SignatureScript))

			sigStr, _, err := PopData(txin.SignatureScript)
			if err != nil {
				io.WriteString(errorFile, fmt.Sprintf(
					"Block %v (%v) tx %v (%v) txin %v (%v)\nError: %v\n%v",
					blkid, sha, i, &txsha, t, "parseData", err,
					spew.Sdump(txin.SignatureScript)))
				continue
			}
			// log.Debugf("tx %v: TxIn %v: sigStr: %v", i, t, spew.Sdump(sigStr))

			signature, err := btcec.ParseSignature(sigStr, btcec.S256())
			if err != nil {
				io.WriteString(errorFile, fmt.Sprintf(
					"Block %v (%v) tx %v (%v) txin %v (%v)\nError: %v\n%v",
					blkid, sha, i, &txsha, t, "ParseSignature", err,
					spew.Sdump(sigStr)))
				continue

			}
			// log.Debugf("tx %v: TxIn %v: signature: %v", i, t, spew.Sdump(signature))

			signatureString := signature.R.String()
			sigId := fmt.Sprintf("%v:%v:%v", blkid, txsha.String()[:6], t)
			if rValuesMap[signatureString] != "" {
				log.Infof("%v DUPLICATE FOUND: %v", sigId, rValuesMap[signatureString])
				if len(duplicates[signatureString]) == 0 {
					duplicates[signatureString] = append(
						duplicates[signatureString], rValuesMap[signatureString])
				}
				duplicates[signatureString] = append(duplicates[signatureString], sigId)
			} else {
				rValuesMap[signatureString] = sigId
				sigCounter++
			}

		}

	}

	return sigCounter, nil
}
