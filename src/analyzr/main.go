package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/conformal/btcec"
	"github.com/conformal/btcnet"
	"github.com/conformal/btcutil"
)

type rData struct {
	Sig       *btcec.Signature
	H         int64
	SigScript []byte
}

func popData(SignatureScript []byte) ([]byte, []byte, error) {
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

func main() {
	var jsonFile = flag.String("json", "blockchainr.json", "blockchainr output")
	flag.Parse()

	blockchainrFile, err := ioutil.ReadFile(*jsonFile)
	if err != nil {
		log.Fatalln("failed to read blockchainr.json:", err)
	}

	results := make(map[string][]*rData)
	err = json.Unmarshal(blockchainrFile, &results)
	if err != nil {
		log.Fatalln("Unmarshal error:", err)
	}

	balanceCache := make(map[string][]byte)

	for r, result := range results {
		log.Println(r)
		for _, rd := range result {
			_, remaining, err := popData(rd.SigScript)
			if err != nil {
				log.Fatalln("PopData failed:", err)
			}
			pkStr, _, err := popData(remaining)
			if err != nil {
				log.Printf("The second PopData failed - probably a pay-to-PubKey:", err)
				// FIX: use recoverKeyFromSignature?
				continue
			}

			aPubKey, err := btcutil.NewAddressPubKey(pkStr, &btcnet.MainNetParams)
			if err != nil {
				log.Printf("Pubkey parse error:", err)
				continue
			}
			address := aPubKey.EncodeAddress()

			balance, ok := balanceCache[address]
			if !ok {
				response, err := http.Get("https://blockchain.info/q/addressbalance/" + address)
				if err != nil {
					log.Fatalln("Get failed:", err)
				}
				defer response.Body.Close()
				balance, err = ioutil.ReadAll(response.Body)
				if err != nil {
					log.Fatalln("ReadAll failed:", err)
				}
				balanceCache[address] = balance
			}

			log.Println(address, string(balance))
		}
	}
}
