// Copyright (c) 2013 Conformal Systems LLC.
// Copyright (c) 2014 Filippo Valsorda
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"github.com/FiloSottile/blockchainr"
	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/ldb"
	"github.com/conformal/btclog"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/conformal/go-flags"
	// "math/big"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

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
		n, err := blockchainr.DumpBlock(h, db, duplicates, rValuesMap, errorFile, log)
		if err != nil {
			log.Warnf("Failed to dump block %v, err %v", h, err)
		} else {
			blocksCounter++
			sigCounter += int64(n)
		}
		if blocksCounter%10000 == 0 {
			log.Infof("%v blocks processed in %v, %v signatures stored",
				blocksCounter, time.Since(last_time), sigCounter)
			last_time = time.Now()
		}
	}

	logResults()
}

func logResults() {
	log.Infof("%v blocks processed, %v signatures stored",
		blocksCounter, sigCounter)

	resultsFile, err := os.Create("blockchainr.json")
	if err != nil {
		log.Warnf("failed to create blockchainr.json: %v", err)
		return
	}
	json_result, err := json.MarshalIndent(duplicates, "", "\t")
	if err != nil {
		log.Warnf("failed to Marshal the result: %v", err)
		return
	}
	resultsFile.Write(json_result)
}
