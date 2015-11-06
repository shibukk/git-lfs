package lfs

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/github/git-lfs/vendor/_nuts/github.com/ThomsonReutersEikon/go-ntlm/ntlm"
)

func (c *Configuration) ntlmClientSession(creds Creds) (ntlm.ClientSession, error) {
	if c.ntlmSession != nil {
		return c.ntlmSession, nil
	}
	splits := strings.Split(creds["username"], "\\")

	if len(splits) != 2 {
		errorMessage := fmt.Sprintf("Your user name must be of the form DOMAIN\\user. It is currently %s", creds["username"], "string")
		return nil, errors.New(errorMessage)
	}

	session, err := ntlm.CreateClientSession(ntlm.Version2, ntlm.ConnectionOrientedMode)

	if err != nil {
		return nil, err
	}

	session.SetUserInfo(splits[1], creds["password"], strings.ToUpper(splits[0]))
	c.ntlmSession = session
	return session, nil
}

func DoNTLMRequest(request *http.Request, retry bool) (*http.Response, error) {
	handReq, err := cloneRequest(request)
	if err != nil {
		return nil, err
	}

	res, err := InitHandShake(handReq)

	if err != nil && res == nil {
		return nil, err
	}

	//If the status is 401 then we need to re-authenticate, otherwise it was successful
	if res.StatusCode == 401 {

		creds, err := getCredsForAPI(request)
		if err != nil {
			return nil, err
		}

		negotiateReq, err := cloneRequest(request)
		if err != nil {
			return nil, err
		}

		challengeMessage, err := negotiate(negotiateReq, getNegotiateMessage())
		if err != nil {
			return nil, err
		}

		challengeReq, err := cloneRequest(request)
		if err != nil {
			return nil, err
		}

		res, err := challenge(challengeReq, challengeMessage, creds)
		if err != nil {
			return nil, err
		}

		//If the status is 401 then we need to re-authenticate
		if res.StatusCode == 401 && retry == true {
			return DoNTLMRequest(challengeReq, false)
		}

		saveCredentials(creds, res)

		return res, nil
	}
	return res, nil
}

func InitHandShake(request *http.Request) (*http.Response, error) {
	return Config.HttpClient().Do(request)
}

func negotiate(request *http.Request, message string) ([]byte, error) {
	request.Header.Add("Authorization", message)
	res, err := Config.HttpClient().Do(request)

	if res == nil && err != nil {
		return nil, err
	}

	ret, err := parseChallengeResponse(res)

	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	defer io.Copy(ioutil.Discard, res.Body)

	return ret, nil
}

func challenge(request *http.Request, challengeBytes []byte, creds Creds) (*http.Response, error) {
	challenge, err := ntlm.ParseChallengeMessage(challengeBytes)

	if err != nil {
		return nil, err
	}

	session, err := Config.ntlmClientSession(creds)
	if err != nil {
		return nil, err
	}

	session.ProcessChallengeMessage(challenge)
	authenticate, err := session.GenerateAuthenticateMessage()

	if err != nil {
		return nil, err
	}

	authenticateMessage := concatS("NTLM ", base64.StdEncoding.EncodeToString(authenticate.Bytes()))
	request.Header.Add("Authorization", authenticateMessage)
	return Config.HttpClient().Do(request)
}

func parseChallengeResponse(response *http.Response) ([]byte, error) {
	header := response.Header.Get("Www-Authenticate")

	//parse out the "NTLM " at the beginning of the response
	challenge := header[5:]
	val, err := base64.StdEncoding.DecodeString(challenge)

	if err != nil {
		return nil, err
	}
	return []byte(val), nil
}

func cloneRequest(request *http.Request) (*http.Request, error) {
	var rdr1, rdr2 myReader
	var clonedReq *http.Request
	var err error

	if request.Body != nil {
		//If we have a body (POST/PUT etc.)
		//We need to do some magic to copy the request without closing the body stream

		buf, err := ioutil.ReadAll(request.Body)

		if err != nil {
			return nil, err
		}

		rdr1 = myReader{bytes.NewBuffer(buf)}
		rdr2 = myReader{bytes.NewBuffer(buf)}
		request.Body = rdr2 // OK since rdr2 implements the io.ReadCloser interface
		clonedReq, err = http.NewRequest(request.Method, request.URL.String(), rdr1)

		if err != nil {
			return nil, err
		}

	} else {
		clonedReq, err = http.NewRequest(request.Method, request.URL.String(), nil)

		if err != nil {
			return nil, err
		}
	}

	for k, v := range request.Header {
		clonedReq.Header.Add(k, v[0])
	}

	clonedReq.ContentLength = request.ContentLength

	return clonedReq, nil
}

func getNegotiateMessage() string {
	return "NTLM TlRMTVNTUAABAAAAB7IIogwADAAzAAAACwALACgAAAAKAAAoAAAAD1dJTExISS1NQUlOTk9SVEhBTUVSSUNB"
}

func concatS(ar ...string) string {

	var buffer bytes.Buffer

	for _, s := range ar {
		buffer.WriteString(s)
	}

	return buffer.String()
}

func concat(ar ...[]byte) []byte {
	return bytes.Join(ar, nil)
}

type myReader struct {
	*bytes.Buffer
}

// So that myReader implements the io.ReadCloser interface
func (m myReader) Close() error { return nil }
