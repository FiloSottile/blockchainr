// Copyright (c) 2014 Filippo Valsorda
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/bitly/dablooms/godablooms"
	"github.com/davecgh/go-spew/spew"

	"github.com/conformal/btcchain"
	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/ldb"
	"github.com/conformal/btcec"
	"github.com/conformal/btclog"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
)

type stringSet map[string]struct{}

func (s stringSet) Add(item string) {
	s[item] = struct{}{}
}

func (s stringSet) Contains(item string) bool {
	_, ok := s[item]
	return ok
}

const (
	tickFreq  = 10
	bloomSize = 100000000
	bloomRate = 0.005
)

func btcdbSetup(dataDir, dbType string) (log btclog.Logger, db btcdb.Db, cleanup func()) {
	// Setup logging
	backendLogger := btclog.NewDefaultBackendLogger()
	log = btclog.NewSubsystemLogger(backendLogger, "")
	btcdb.UseLogger(log)

	// Setup database access
	blockDbNamePrefix := "blocks"
	dbName := blockDbNamePrefix + "_" + dbType
	if dbType == "sqlite" {
		dbName = dbName + ".db"
	}
	dbPath := filepath.Join(dataDir, "mainnet", dbName)

	log.Infof("loading db %v", dbType)
	db, err := btcdb.OpenDB(dbType, dbPath)
	if err != nil {
		log.Warnf("db open failed: %v", err)
		return
	}
	log.Infof("db load complete")

	cleanup = func() {
		db.Close()
		backendLogger.Flush()
	}

	return
}

type rData struct {
	sig  *btcec.Signature
	H    int64
	Tx   int
	TxIn int
}

func getSignatures(maxHeigth int64, errorFile io.Writer, log btclog.Logger, db btcdb.Db) chan *rData {
	c := make(chan *rData)

	go func() {
		for h := int64(0); h < maxHeigth; h++ {
			sha, err := db.FetchBlockShaByHeight(h)
			if err != nil {
				log.Warnf("failed FetchBlockShaByHeight(%v): %v", h, err)
				return
			}
			blk, err := db.FetchBlockBySha(sha)
			if err != nil {
				log.Warnf("failed FetchBlockBySha(%v) - h %v: %v", sha, h, err)
				return
			}

			mblk := blk.MsgBlock()

			for i, tx := range mblk.Transactions {
				// txsha, err := tx.TxSha()
				// if err != nil {
				// 	log.Warnf("Block %v (%v)", h, sha)
				// 	log.Warnf("tx %v (%v)", i, &txsha)
				// 	log.Warnf("Error: %v", err)
				// 	continue
				// }

				if btcchain.IsCoinBase(btcutil.NewTx(tx)) {
					continue
				}

				for t, txin := range tx.TxIn {
					data, err := btcscript.PushedData(txin.SignatureScript)
					if err != nil {
						io.WriteString(errorFile, fmt.Sprintf(
							"Block %v (%v) tx %v txin %v (%v)\nError: %v\n%v",
							h, sha, i, t, "PushedData", err,
							spew.Sdump(txin.SignatureScript)))
						continue
					}

					if len(data) == 0 {
						continue
					}

					signature, err := btcec.ParseSignature(data[0], btcec.S256())
					if err != nil {
						io.WriteString(errorFile, fmt.Sprintf(
							"Block %v (%v) tx %v txin %v (%v)\nError: %v\n%v",
							h, sha, i, t, "ParseSignature", err,
							spew.Sdump(data)))
						continue
					}

					c <- &rData{
						sig:  signature,
						H:    h,
						Tx:   i,
						TxIn: t,
					}
				}
			}
		}

		close(c)
	}()

	return c
}

func search(log btclog.Logger, db btcdb.Db, errorFile io.Writer) map[string][]*rData {
	// Setup signal handler
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	// Potential optimisation: keep the bloom filter between runs
	filter := dablooms.NewScalingBloom(bloomSize, bloomRate, "blockchainr_bloom.bin")
	if filter == nil {
		log.Warn("dablooms.NewScalingBloom failed")
		return nil
	}

	potentialValues := make(stringSet)
	rMap := make(map[string][]*rData)

	_, maxHeigth, err := db.NewestSha()
	if err != nil {
		log.Warnf("db NewestSha failed: %v", err)
		return nil
	}

	for step := 1; step <= 2; step++ {
		lastTime := time.Now()
		lastSig := int64(0)
		sigCounter := int64(0)
		matches := int64(0)
		ticker := time.Tick(tickFreq * time.Second)

		signatures := getSignatures(maxHeigth, errorFile, log, db)
		for rd := range signatures {
			select {
			case s := <-signalChan:
				log.Infof("Step %v - signal %v - %v sigs in %.2fs, %v matches, %v total, block %v of %v",
					step, s, sigCounter-lastSig, time.Since(lastTime).Seconds(),
					matches, sigCounter, rd.H, maxHeigth)

				if s == syscall.SIGINT || s == syscall.SIGTERM {
					return rMap
				}

			case <-ticker:
				log.Infof("Step %v - %v sigs in %.2fs, %v matches, %v total, block %v of %v",
					step, sigCounter-lastSig, time.Since(lastTime).Seconds(),
					matches, sigCounter, rd.H, maxHeigth)
				lastTime = time.Now()
				lastSig = sigCounter

			default:
				break
			}

			// Potential optimisation: store in potentialValues also the block
			// height, and if step 2 finds the same h first, it's a bloom
			// false positive
			if step == 1 {
				b := rd.sig.R.Bytes()
				if filter.Check(b) {
					matches++
					potentialValues.Add(rd.sig.R.String())
				} else {
					if !filter.Add(b, 1) {
						log.Warn("Add failed (?)")
					}
				}
			} else if step == 2 {
				if potentialValues.Contains(rd.sig.R.String()) {
					matches++
					rMap[rd.sig.R.String()] = append(rMap[rd.sig.R.String()], rd)
				}
			}
			sigCounter++
		}

		if *memprofile != "" {
			f, err := os.Create(fmt.Sprintf("%s.%d", *memprofile, step))
			if err != nil {
				log.Warnf("open memprofile failed: %v", err)
				return nil
			}
			pprof.WriteHeapProfile(f)
			f.Close()
		}

		log.Infof("Step %v done - %v signatures processed - %v matches",
			step, sigCounter, matches)
	}
	return rMap
}

var (
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
	memprofile = flag.String("memprofile", "", "write memory profile to this file")
)

func main() {
	var (
		dataDir = flag.String("datadir", filepath.Join(btcutil.AppDataDir("btcd", false), "data"), "BTCD: Data directory")
		dbType  = flag.String("dbtype", "leveldb", "BTCD: Database backend")
	)
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Setup btcdb
	log, db, dbCleanup := btcdbSetup(*dataDir, *dbType)
	defer dbCleanup()

	errorFile, err := os.Create("blockchainr_error.log")
	if err != nil {
		log.Warnf("failed to create blockchainr_error.log: %v", err)
		return
	}
	defer errorFile.Close()

	duplicates := search(log, db, errorFile)

	realDuplicates := make(map[string][]*rData)
	for k, v := range duplicates {
		if len(v) > 1 {
			realDuplicates[k] = v
		}
	}

	resultsFile, err := os.Create("blockchainr.json")
	if err != nil {
		log.Warnf("failed to create blockchainr.json: %v", err)
		return
	}
	if json.NewEncoder(resultsFile).Encode(realDuplicates) != nil {
		log.Warnf("failed to Encode the result: %v", err)
		return
	}
}
