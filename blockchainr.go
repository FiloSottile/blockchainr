// Copyright (c) 2013 Conformal Systems LLC.
// Copyright (c) 2014 Filippo Valsorda
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
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
	"os/signal"
	"path/filepath"
	"time"
)

// TODO: monky-patch OP_CHECKSIG

type ShaHash btcwire.ShaHash

type config struct {
	DataDir  string `short:"b" long:"datadir" description:"Directory to store data"`
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

var (
	rValuesMap = make(map[string]string)
	duplicates = make(map[string][]string)

	blocksCounter int64 = 0
	sigCounter    int64 = 0

	errorFile *os.File
)

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

	signal_chan := make(chan os.Signal, 1)
	signal.Notify(signal_chan, os.Interrupt, os.Kill)

	go func() {
		_ = <-signal_chan
		logResults()
		// this will have to be reworked while parallelizing
		backendLogger.Flush()
		db.Close()
		os.Exit(2)
	}()

	_, max_heigth, err := db.NewestSha()
	if err != nil {
		log.Warnf("db NewestSha failed: %v", err)
		return
	}
	log.Infof("max_heigth: %v", max_heigth)

	errorFile, err = os.Create("blockchainr_error.log")
	if err != nil {
		log.Warnf("failed to create blockchainr_error.log: %v", err)
		return
	}

	last_time := time.Now()
	for h := int64(0); h < max_heigth; h++ {
		// TODO: parallelize
		err = DumpBlock(db, h)
		if err != nil {
			log.Warnf("Failed to dump block %v, err %v", h, err)
		}
		if blocksCounter%10000 == 0 {
			log.Infof("%v blocks processed in %v, %v signatures stored",
				blocksCounter, time.Since(last_time), sigCounter)
			last_time = time.Now()
		}
	}

	logResults()
}

func logScriptError(blkid int64, sha *btcwire.ShaHash, i int, txsha *btcwire.ShaHash, t int, f string, err error, data []byte) {
	errorFile.WriteString(fmt.Sprintf(
		"Block %v (%v) tx %v (%v) txin %v (%v)\nError: %v\n%v",
		blkid, sha, i, txsha, t, f, err, spew.Sdump(data)))
}

func logResults() {
	log.Infof("%v blocks processed, %v signatures stored",
		blocksCounter, sigCounter)

	resultsFile, err := os.Create("blockchainr.txt")
	if err != nil {
		log.Warnf("failed to create blockchainr.txt: %v", err)
		return
	}
	resultsFile.WriteString(spew.Sdump(duplicates))
}

func popData(SignatureScript []uint8) ([]uint8, error) {
	if len(SignatureScript) < 1 {
		return nil, fmt.Errorf("empty SignatureScript")
	}
	opcode := SignatureScript[0]

	if opcode >= 1 && opcode <= 75 {
		if len(SignatureScript) < int(opcode+1) {
			return nil, fmt.Errorf("SignatureScript too short")
		}
		sigStr := SignatureScript[1 : opcode+1]
		return sigStr, nil
	}

	// TODO: OP_PUSHDATA1 OP_PUSHDATA2 OP_PUSHDATA3
	if opcode >= 76 && opcode <= 78 {
		return nil, fmt.Errorf("FIXME: OP_PUSHDATA %v", opcode)
	}

	return nil, fmt.Errorf("the first opcode (%x) is not a data push", opcode)
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
		return fmt.Errorf("WHAT!?")
	}

	log.Debugf("Block %v depth %v", sha, blkid)

	log.Debugf("Block %v depth %v %v", sha, blkid, spew.Sdump(rblk))
	mblk := blk.MsgBlock()
	log.Debugf("Block %v depth %v %v", sha, blkid, spew.Sdump(mblk))

	log.Debugf("Num transactions %v", len(mblk.Transactions))
	for i, tx := range mblk.Transactions {

		txsha, err := tx.TxSha()
		if err != nil {
			log.Warnf("Block %v (%v)", blkid, sha)
			log.Warnf("tx %v (%v)", i, &txsha)
			log.Warnf("Error: %v", err)
			continue
		}

		log.Debugf("tx %v: %v", i, &txsha)

		if btcchain.IsCoinBase(btcutil.NewTx(tx)) {
			log.Debugf("tx %v: skipping (coinbase)", i)
			continue
		}

		for t, txin := range tx.TxIn {
			log.Debugf("tx %v: TxIn %v: SignatureScript: %v", i, t, spew.Sdump(txin.SignatureScript))

			sigStr, err := popData(txin.SignatureScript)
			if err != nil {
				logScriptError(blkid, sha, i, &txsha, t,
					"parseData", err, txin.SignatureScript)
				continue
			}
			log.Debugf("tx %v: TxIn %v: sigStr: %v", i, t, spew.Sdump(sigStr))

			signature, err := btcec.ParseSignature(sigStr, btcec.S256())
			if err != nil {
				logScriptError(blkid, sha, i, &txsha, t,
					"ParseSignature", err, sigStr)
				continue

			}
			log.Debugf("tx %v: TxIn %v: signature: %v", i, t, spew.Sdump(signature))

			signatureString := signature.R.String()
			sigId := fmt.Sprintf("%v:%v:%v", blkid, txsha.String()[:5], t)
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

	blocksCounter++

	return nil
}
