package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"time"
	"flag"

	"github.com/edgelesssys/ego/enclave"
)

func main() {

	isSim := flag.String("m", "0", "is simulation mode")
	flag.Parse()

	// Create certificate and a report that includes the certificate's hash.
	cert, priv := createCertificate()
	hash := sha256.Sum256(cert)

	// if we are in sumulation mode, let's return a byte array of the hash
	var report []byte
	var err error

	if *isSim == "1" {
		os.Setenv("OE_SIMULATION", "1")
		report, err = createSimulationReport(hash[:])
	} else {
		report, err = enclave.GetRemoteReport(hash[:])
	}

	if err != nil {
		fmt.Println(err)
	}

	// Create HTTPS server.
	http.HandleFunc("/cert", func(w http.ResponseWriter, r *http.Request) { w.Write(cert) })
	http.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) { w.Write(report) })
	http.HandleFunc("/secret", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%v sent secret %v\n", r.RemoteAddr, r.URL.Query()["s"])
	})

	tlsCfg := tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{cert},
				PrivateKey:  priv,
			},
		},
	}

	server := http.Server{Addr: "0.0.0.0:8080", TLSConfig: &tlsCfg}

	fmt.Println("listening ...")
	err = server.ListenAndServeTLS("", "")
	fmt.Println(err)
}

func createCertificate() ([]byte, crypto.PrivateKey) {
	template := &x509.Certificate{
		SerialNumber: &big.Int{},
		Subject:      pkix.Name{CommonName: "localhost"},
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	cert, _ := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	return cert, priv
}

func createSimulationReport(data []byte) ([]byte, error) {
	return data, nil // just return the data
}
