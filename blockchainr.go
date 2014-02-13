// Copyright (c) 2013 Conformal Systems LLC.
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"github.com/conformal/btcchain"
	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/ldb"
	"github.com/conformal/btcec"
	"github.com/conformal/btclog"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/conformal/go-flags"
	"github.com/davecgh/go-spew/spew"
	// "math/big"
	"os"
	"path/filepath"
)

type ShaHash btcwire.ShaHash

type config struct {
	DataDir  string `long:"datadir" description:"Directory to store data"`
	DbType   string `long:"dbtype" description:"Database backend"`
	TestNet3 bool   `long:"testnet" description:"Use the test network"`
	// Height   int64  `short:"b" description:"Block height to process" required:"true"`
}

var (
	btcdHomeDir    = btcutil.AppDataDir("btcd", false)
	defaultDataDir = filepath.Join(btcdHomeDir, "data")
	log            btclog.Logger
)

const (
	ArgSha = iota
	ArgHeight
)

var rValuesMap = make(map[string]int64)
var duplicates = make(map[string][]int64)

func main() {
	cfg := config{
		DbType:  "leveldb",
		DataDir: defaultDataDir,
	}
	parser := flags.NewParser(&cfg, flags.Default)
	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return
	}

	backendLogger := btclog.NewDefaultBackendLogger()
	defer backendLogger.Flush()
	log = btclog.NewSubsystemLogger(backendLogger, "")
	btcdb.UseLogger(log)

	var testnet string
	if cfg.TestNet3 {
		testnet = "testnet"
	} else {
		testnet = "mainnet"
	}

	cfg.DataDir = filepath.Join(cfg.DataDir, testnet)

	blockDbNamePrefix := "blocks"
	dbName := blockDbNamePrefix + "_" + cfg.DbType
	if cfg.DbType == "sqlite" {
		dbName = dbName + ".db"
	}
	dbPath := filepath.Join(cfg.DataDir, dbName)

	log.Infof("loading db %v", cfg.DbType)
	db, err := btcdb.OpenDB(cfg.DbType, dbPath)
	if err != nil {
		log.Warnf("db open failed: %v", err)
		return
	}
	defer db.Close()
	log.Infof("db load complete")

	_, max_heigth, err := db.NewestSha()
	if err != nil {
		log.Warnf("db NewestSha failed: %v", err)
		return
	}
	log.Infof("max_heigth: %v", max_heigth)

	for h := int64(0); h < max_heigth; h++ {
		err = DumpBlock(db, h)
		if err != nil {
			log.Warnf("Failed to dump block %v, err %v", h, err)
		}
	}

	log.Info(spew.Sdump(duplicates))
}

var notDataError = errors.New("the first opcode is not a data push")

func parseData(SignatureScript []uint8) ([]uint8, error) {
	opcode := SignatureScript[0]

	if opcode >= 1 && opcode <= 75 {
		sigStr := SignatureScript[1 : opcode+1]
		return sigStr, nil
	}

	// TODO: OP_PUSHDATA1 OP_PUSHDATA2 OP_PUSHDATA3

	return nil, notDataError
}

func DumpBlock(db btcdb.Db, height int64) error {
	sha, err := db.FetchBlockShaByHeight(height)
	if err != nil {
		return err
	}
	blk, err := db.FetchBlockBySha(sha)
	if err != nil {
		return err
	}
	rblk, err := blk.Bytes()
	if err != nil {
		return err
	}
	blkid := blk.Height()
	if blkid != height {
		return errors.New("WHAT!?")
	}

	log.Infof("Block %v depth %v", sha, blkid)

	log.Debugf("Block %v depth %v %v", sha, blkid, spew.Sdump(rblk))
	mblk := blk.MsgBlock()
	log.Debugf("Block %v depth %v %v", sha, blkid, spew.Sdump(mblk))

	log.Infof("Num transactions %v", len(mblk.Transactions))
	for i, tx := range mblk.Transactions {

		txsha, err := tx.TxSha()
		if err != nil {
			log.Warnf("Block %v (%v)", blkid, sha)
			log.Warnf("tx %v (%v)", i, &txsha)
			log.Warnf("Error: %v", err)
			continue
		}

		log.Infof("tx %v: %v", i, &txsha)

		if btcchain.IsCoinBase(btcutil.NewTx(tx)) {
			log.Infof("tx %v: skipping (coinbase)", i)
			continue
		}

		for t, txin := range tx.TxIn {
			log.Debugf("tx %v: TxIn %v: SignatureScript: %v", i, t, spew.Sdump(txin.SignatureScript))

			sigStr, err := parseData(txin.SignatureScript)
			if err != nil {
				log.Warnf("Block %v (%v)", blkid, sha)
				log.Warnf("tx %v (%v)", i, &txsha)
				log.Warnf("txin %v (parseData)", t)
				log.Warnf("Error: %v", err)
				continue
			}
			log.Debugf("tx %v: TxIn %v: sigStr: %v", i, t, spew.Sdump(sigStr))

			signature, err := btcec.ParseSignature(sigStr, btcec.S256())
			if err != nil {
				log.Warnf("Block %v (%v)", blkid, sha)
				log.Warnf("tx %v (%v)", i, &txsha)
				log.Warnf("txin %v (ParseSignature)", t)
				log.Warnf("Error: %v", err)
				continue

			}
			log.Infof("tx %v: TxIn %v: signature: %v", i, t, spew.Sdump(signature))

			signatureString := signature.R.String()
			if rValuesMap[signatureString] != int64(0) {
				log.Infof("DUPLICATE FOUND: %v", rValuesMap[signatureString])
				if len(duplicates[signatureString]) == 0 {
					duplicates[signatureString] = append(duplicates[signatureString], rValuesMap[signatureString])
				}
				duplicates[signatureString] = append(duplicates[signatureString], blkid)
			} else {
				rValuesMap[signatureString] = blkid
			}

		}

	}

	return nil
}
