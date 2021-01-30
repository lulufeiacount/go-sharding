/*
Copyright 2019 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mysql

import (
	"fmt"
	"github.com/XiaoMi/Gaea/mysql/types"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isUnix() bool {
	sysType := runtime.GOOS

	return strings.ToLower(sysType) != "windows"

}

var testUserProvider = NewStaticUserProvider("user1", "password1")
var testCounter = NewTestTelemetry()

var selectRowsResult = &types.Result{
	Fields: []*types.Field{
		{
			Name: "id",
			Type: types.Int32,
		},
		{
			Name: "name",
			Type: types.VarChar,
		},
	},
	Rows: [][]types.Value{
		{
			types.MakeTrusted(types.Int32, []byte("10")),
			types.MakeTrusted(types.VarChar, []byte("nice name")),
		},
		{
			types.MakeTrusted(types.Int32, []byte("20")),
			types.MakeTrusted(types.VarChar, []byte("nicer name")),
		},
	},
	RowsAffected: 2,
}

func newTestListenerWithCounter(telemetry ConnTelemetry) (*Listener, error) {
	conf := ListenerConfig{
		Protocol:         "tcp4",
		Address:          ":0",
		Handler:          &testHandler{},
		ConnReadTimeout:  0,
		ConnWriteTimeout: 0,
		Telemetry:        telemetry,
	}

	l, err := NewListenerWithConfig(conf, NewStaticUserProvider("user1", "password1"))
	return l, err
}

func newTestListenerWithHandler(handler Handler) (*Listener, error) {
	conf := ListenerConfig{
		Protocol:         "tcp4",
		Address:          ":0",
		Handler:          handler,
		ConnReadTimeout:  0,
		ConnWriteTimeout: 0,
		Telemetry:        testCounter,
	}

	l, err := NewListenerWithConfig(conf, NewStaticUserProvider("user1", "password1"))
	return l, err
}

func newTestListenerDefault() (*Listener, error) {
	conf := ListenerConfig{
		Protocol:         "tcp4",
		Address:          ":0",
		Handler:          &testHandler{},
		ConnReadTimeout:  0,
		ConnWriteTimeout: 0,
		Telemetry:        testCounter,
	}

	l, err := NewListenerWithConfig(conf, NewStaticUserProvider("user1", "password1"))
	return l, err
}

func newDefaultConnParam(t *testing.T, l *Listener) *ConnParams {
	_, port := getHostPort(t, l.Addr())

	// Setup the right parameters.
	params := &ConnParams{
		Host:  "127.0.0.1",
		Port:  port,
		Uname: "user1",
		Pass:  "password1",
	}
	return params
}

func newDefaultConnParamWithDb(t *testing.T, l *Listener, db string) *ConnParams {
	_, port := getHostPort(t, l.Addr())

	// Setup the right parameters.
	params := &ConnParams{
		Host:   "127.0.0.1",
		Port:   port,
		Uname:  "user1",
		Pass:   "password1",
		DbName: db,
	}
	return params
}

func newTestListener(user string, passwd string) (*Listener, error) {
	conf := ListenerConfig{
		Protocol:         "tcp4",
		Address:          ":0",
		Handler:          &testHandler{},
		ConnReadTimeout:  0,
		ConnWriteTimeout: 0,
		Telemetry:        testCounter,
	}

	l, err := NewListenerWithConfig(conf, NewStaticUserProvider(user, passwd))
	return l, err
}

func getHostPort(t *testing.T, a net.Addr) (string, int) {
	// For the host name, we resolve 'localhost' into an address.
	// This works around a few travis issues where IPv6 is not 100% enabled.
	hosts, err := net.LookupHost("localhost")
	if err != nil {
		t.Fatalf("LookupHost(localhost) failed: %v", err)
	}
	host := hosts[0]
	port := a.(*net.TCPAddr).Port
	t.Logf("listening on address '%v' port %v", host, port)
	return host, port
}

func TestConnectionFromListener(t *testing.T) {

	l, err := newTestListenerDefault()
	if err != nil {
		t.Fatalf("NewListener failed: %v", err)
	}
	defer l.Close()
	go l.Accept()

	params := newDefaultConnParamWithDb(t, l, "sharding-db")

	c, err := Connect(context.Background(), params)
	if err != nil {
		t.Errorf("Should be able to connect to server but found error: %v", err)
	}
	c.Close()
}

func TestConnectionWithoutSourceHost(t *testing.T) {

	l, err := newTestListenerDefault()
	if err != nil {
		t.Fatalf("NewListener failed: %v", err)
	}
	defer l.Close()
	go l.Accept()

	getHostPort(t, l.Addr())
	//Setup the right parameters.
	params := newDefaultConnParam(t, l)

	c, err := Connect(context.Background(), params)
	if err != nil {
		t.Errorf("Should be able to connect to server but found error: %v", err)
	}
	c.Close()
}

func TestConnectionUnixSocket(t *testing.T) {
	if isUnix() {
		th := &testHandler{}

		unixSocket, err := ioutil.TempFile("", "mysql_vitess_test.sock")
		if err != nil {
			t.Fatalf("Failed to create temp file")
		}
		os.Remove(unixSocket.Name())

		l, err := NewListener("unix", unixSocket.Name(), th, 0, 0, testUserProvider)
		if err != nil {
			t.Fatalf("NewListener failed: %v", err)
		}
		defer l.Close()
		go l.Accept()

		// Setup the right parameters.
		params := &ConnParams{
			UnixSocket: unixSocket.Name(),
			Uname:      "user1",
			Pass:       "password1",
		}

		c, err := Connect(context.Background(), params)
		if err != nil {
			t.Errorf("Should be able to connect to server but found error: %v", err)
		}
		c.Close()
	}
}

func TestClientFoundRows(t *testing.T) {
	th := &testHandler{}

	l, err := newTestListenerWithHandler(th)
	if err != nil {
		t.Fatalf("NewListener failed: %v", err)
	}
	defer l.Close()
	go l.Accept()

	// Setup the right parameters.
	params := newDefaultConnParam(t, l)

	// Test without flag.
	c, err := Connect(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	foundRows := th.LastConn().Capabilities & CapabilityClientFoundRows
	if foundRows != 0 {
		t.Errorf("FoundRows flag: %x, second bit must be 0", th.LastConn().Capabilities)
	}
	c.Close()
	if !c.IsClosed() {
		t.Errorf("IsClosed returned true on Close-d connection.")
	}

	// Test with flag.
	params.Flags |= CapabilityClientFoundRows
	c, err = Connect(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	foundRows = th.LastConn().Capabilities & CapabilityClientFoundRows
	if foundRows == 0 {
		t.Errorf("FoundRows flag: %x, second bit must be set", th.LastConn().Capabilities)
	}
	c.Close()
}

func TestConnCounts(t *testing.T) {

	initialNumUsers := len(testCounter.ConnCountPerUser)

	l, err := newTestListener("anotherNotYetConnectedUser1", "oh")
	if err != nil {
		t.Fatalf("NewListener failed: %v", err)
	}
	defer l.Close()
	go l.Accept()

	// Test with one new connection.
	params := newDefaultConnParam(t, l)
	params.Uname = "anotherNotYetConnectedUser1"
	params.Pass = "oh"

	c, err := Connect(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}

	connCounts := len(testCounter.ConnCountPerUser)
	if count := connCounts; count-initialNumUsers != 1 {
		t.Errorf("Expected 1 new user, got %d (init: %d)", count, initialNumUsers)
	}
	checkCountsForUser(t, params.Uname, 1)

	// Test with a second new connection.
	c2, err := Connect(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}

	connCounts = len(testCounter.ConnCountPerUser)
	// There is still only one new user.
	if l2 := connCounts; l2-initialNumUsers != 1 {
		t.Errorf("Expected 1 new user, got %d", l2)
	}
	checkCountsForUser(t, params.Uname, 2)

	// Test after closing connections. time.Sleep lets it work, but seems flakey.
	c.Close()
	//time.Sleep(10 * time.Millisecond)
	//checkCountsForUser(t, user, 1)

	c2.Close()
	//time.Sleep(10 * time.Millisecond)
	//checkCountsForUser(t, user, 0)
}

func checkCountsForUser(t *testing.T, user string, expected int64) {
	connCounts := testCounter.ConnCountPerUser

	userCount, ok := connCounts[user]
	if ok {
		if userCount != expected {
			t.Errorf("Expected connection count for user to be %d, got %d", expected, userCount)
		}
	} else {
		t.Errorf("No count found for user %s", user)
	}
}

func TestServer(t *testing.T) {
	if !isUnix() {
		return
	}
	th := &testHandler{}

	l, err := newTestListenerDefault()
	require.NoError(t, err)
	l.SlowConnectWarnThreshold.Set(time.Duration(time.Nanosecond * 1))
	defer l.Close()
	go l.Accept()

	// Setup the right parameters.
	params := newDefaultConnParam(t, l)

	// Run a 'select rows' command with results.
	output, err := runMysqlWithErr(t, params, "select rows")
	require.NoError(t, err)

	if !strings.Contains(output, "nice name") ||
		!strings.Contains(output, "nicer name") ||
		!strings.Contains(output, "2 rows in set") {
		t.Errorf("Unexpected output for 'select rows'")
	}
	assert.NotContains(t, output, "warnings")

	// Run a 'select rows' command with warnings
	th.SetWarnings(13)
	output, err = runMysqlWithErr(t, params, "select rows")
	require.NoError(t, err)
	if !strings.Contains(output, "nice name") ||
		!strings.Contains(output, "nicer name") ||
		!strings.Contains(output, "2 rows in set") ||
		!strings.Contains(output, "13 warnings") {
		t.Errorf("Unexpected output for 'select rows': %v", output)
	}
	th.SetWarnings(0)

	// If there's an error after streaming has started,
	// we should get a 2013
	th.SetErr(NewSQLError(ERUnknownComError, SSUnknownComError, "forced error after send"))
	output, err = runMysqlWithErr(t, params, "error after send")
	require.Error(t, err)
	if !strings.Contains(output, "ERROR 2013 (HY000)") ||
		!strings.Contains(output, "Lost connection to MySQL server during query") {
		t.Errorf("Unexpected output for 'panic'")
	}

	// Run an 'insert' command, no rows, but rows affected.
	output, err = runMysqlWithErr(t, params, "insert")
	require.NoError(t, err)
	if !strings.Contains(output, "Query OK, 123 rows affected") {
		t.Errorf("Unexpected output for 'insert'")
	}

	// Run a 'schema echo' command, to make sure db name is right.
	params.DbName = "XXXfancyXXX"
	output, err = runMysqlWithErr(t, params, "schema echo")
	require.NoError(t, err)
	if !strings.Contains(output, params.DbName) {
		t.Errorf("Unexpected output for 'schema echo'")
	}

	// Sanity check: make sure this didn't go through SSL
	output, err = runMysqlWithErr(t, params, "ssl echo")
	require.NoError(t, err)
	if !strings.Contains(output, "ssl_flag") ||
		!strings.Contains(output, "OFF") ||
		!strings.Contains(output, "1 row in set") {
		t.Errorf("Unexpected output for 'ssl echo': %v", output)
	}

	// UserData check: checks the server user data is correct.
	output, err = runMysqlWithErr(t, params, "userData echo")
	require.NoError(t, err)
	if !strings.Contains(output, "user1") ||
		!strings.Contains(output, "user_data") ||
		!strings.Contains(output, "userData1") {
		t.Errorf("Unexpected output for 'userData echo': %v", output)
	}

	// Permissions check: check a bad password is rejected.
	params.Pass = "bad"
	output, err = runMysqlWithErr(t, params, "select rows")
	require.Error(t, err)
	if !strings.Contains(output, "1045") ||
		!strings.Contains(output, "28000") ||
		!strings.Contains(output, "Access denied") {
		t.Errorf("Unexpected output for invalid password: %v", output)
	}

	// Permissions check: check an unknown user is rejected.
	params.Pass = "password1"
	params.Uname = "user2"
	output, err = runMysqlWithErr(t, params, "select rows")
	require.Error(t, err)
	if !strings.Contains(output, "1045") ||
		!strings.Contains(output, "28000") ||
		!strings.Contains(output, "Access denied") {
		t.Errorf("Unexpected output for invalid password: %v", output)
	}

	// Uncomment to leave setup up for a while, to run tests manually.
	//	fmt.Printf("Listening to server on host '%v' port '%v'.\n", host, port)
	//	time.Sleep(60 * time.Minute)
}

// TestClearTextServer creates a Server that needs clear text
// passwords from the client.
func TestClearTextServer(t *testing.T) {
	if !isUnix() {
		return
	}
	th := &testHandler{}

	l, err := NewListener("tcp", ":0", th, 0, 0, testUserProvider)
	if err != nil {
		t.Fatalf("NewListener failed: %v", err)
	}
	defer l.Close()
	go l.Accept()

	host, port := getHostPort(t, l.Addr())

	version, _ := runMysql(t, nil, "--version")
	isMariaDB := strings.Contains(version, "MariaDB")

	// Setup the right parameters.
	params := &ConnParams{
		Host:  host,
		Port:  port,
		Uname: "user1",
		Pass:  "password1",
	}

	// Run a 'select rows' command with results.  This should fail
	// as clear text is not enabled by default on the client
	// (except MariaDB).
	l.AllowClearTextWithoutTLS.Set(true)
	sql := "select rows"
	output, ok := runMysql(t, params, sql)
	if ok {
		if isMariaDB {
			t.Logf("mysql should have failed but returned: %v\nbut letting it go on MariaDB", output)
		} else {
			t.Fatalf("mysql should have failed but returned: %v", output)
		}
	} else {
		if strings.Contains(output, "No such file or directory") {
			t.Logf("skipping mysql clear text tests, as the clear text plugin cannot be loaded: %v", err)
			return
		}
		if !strings.Contains(output, "plugin not enabled") {
			t.Errorf("Unexpected output for 'select rows': %v", output)
		}
	}

	// Now enable clear text plugin in client, but server requires SSL.
	l.AllowClearTextWithoutTLS.Set(false)
	if !isMariaDB {
		sql = enableCleartextPluginPrefix + sql
	}
	output, ok = runMysql(t, params, sql)
	if ok {
		t.Fatalf("mysql should have failed but returned: %v", output)
	}
	if !strings.Contains(output, "Cannot use clear text authentication over non-SSL connections") {
		t.Errorf("Unexpected output for 'select rows': %v", output)
	}

	// Now enable clear text plugin, it should now work.
	l.AllowClearTextWithoutTLS.Set(true)
	output, ok = runMysql(t, params, sql)
	if !ok {
		t.Fatalf("mysql failed: %v", output)
	}
	if !strings.Contains(output, "nice name") ||
		!strings.Contains(output, "nicer name") ||
		!strings.Contains(output, "2 rows in set") {
		t.Errorf("Unexpected output for 'select rows'")
	}

	// Change password, make sure server rejects us.
	params.Pass = "bad"
	output, ok = runMysql(t, params, sql)
	if ok {
		t.Fatalf("mysql should have failed but returned: %v", output)
	}
	if !strings.Contains(output, "Access denied for user 'user1'") {
		t.Errorf("Unexpected output for 'select rows': %v", output)
	}
}

// TestTLSServer creates a Server with TLS support, then uses mysql
// client to connect to it.
//func TestTLSServer(t *testing.T) {
//	th := &testHandler{}
//
//	// Create the listener, so we can get its host.
//	// Below, we are enabling --ssl-verify-server-cert, which adds
//	// a check that the common name of the certificate matches the
//	// server host name we connect to.
//	l, err := NewListener("tcp", ":0", th, 0, 0, testUserProvider)
//	if err != nil {
//		t.Fatalf("NewListener failed: %v", err)
//	}
//	defer l.Close()
//
//	// Make sure hostname is added as an entry to /etc/hosts, otherwise ssl handshake will fail
//	host, err := os.Hostname()
//	if err != nil {
//		t.Fatalf("Failed to get os Hostname: %v", err)
//	}
//
//	port := l.Addr().(*net.TCPAddr).Port
//
//	// Create the certs.
//	root, err := ioutil.TempDir("", "TestTLSServer")
//	if err != nil {
//		t.Fatalf("TempDir failed: %v", err)
//	}
//	defer os.RemoveAll(root)
//	tlstest.CreateCA(root)
//	tlstest.CreateSignedCert(root, tlstest.CA, "01", "server", host)
//	tlstest.CreateSignedCert(root, tlstest.CA, "02", "client", "Client Cert")
//
//	// Create the server with TLS config.
//	serverConfig, err := ServerConfig(
//		path.Join(root, "server-cert.pem"),
//		path.Join(root, "server-key.pem"),
//		path.Join(root, "ca-cert.pem"))
//	if err != nil {
//		t.Fatalf("TLSServerConfig failed: %v", err)
//	}
//	l.TLSConfig.Store(serverConfig)
//	go l.Accept()
//
//	// Setup the right parameters.
//	params := &ConnParams{
//		Host:  host,
//		Port:  port,
//		Uname: "user1",
//		Pass:  "password1",
//		// SSL flags.
//		Flags:   CapabilityClientSSL,
//		SslCa:   path.Join(root, "ca-cert.pem"),
//		SslCert: path.Join(root, "client-cert.pem"),
//		SslKey:  path.Join(root, "client-key.pem"),
//	}
//
//	// Run a 'select rows' command with results.
//	conn, err := Connect(context.Background(), params)
//	//output, ok := runMysql(t, params, "select rows")
//	if err != nil {
//		t.Fatalf("mysql failed: %v", err)
//	}
//	results, err := conn.ExecuteFetch("select rows", 1000, true)
//	if err != nil {
//		t.Fatalf("mysql fetch failed: %v", err)
//	}
//	output := ""
//	for _, row := range results.Rows {
//		r := make([]string, 0)
//		for _, col := range row {
//			r = append(r, col.String())
//		}
//		output = output + strings.Join(r, ",") + "\n"
//	}
//
//	if results.Rows[0][1].ToString() != "nice name" ||
//		results.Rows[1][1].ToString() != "nicer name" ||
//		len(results.Rows) != 2 {
//		t.Errorf("Unexpected output for 'select rows': %v", output)
//	}
//
//	// make sure this went through SSL
//	results, err = conn.ExecuteFetch("ssl echo", 1000, true)
//	if err != nil {
//		t.Fatalf("mysql fetch failed: %v", err)
//	}
//	if results.Rows[0][0].ToString() != "ON" {
//		t.Errorf("Unexpected output for 'ssl echo': %v", results)
//	}
//
//	// Find out which TLS version the connection actually used,
//	// so we can check that the corresponding counter was incremented.
//	tlsVersion := conn.conn.(*tls.Conn).ConnectionState().Version
//
//	checkCountForTLSVer(t, tlsVersionToString(tlsVersion), 1)
//	checkCountForTLSVer(t, versionNoTLS, 0)
//	conn.Close()
//
//}

// TestTLSRequired creates a Server with TLS required, then tests that an insecure mysql
// client is rejected
//func TestTLSRequired(t *testing.T) {
//	th := &testHandler{}
//
//	// Create the listener, so we can get its host.
//	// Below, we are enabling --ssl-verify-server-cert, which adds
//	// a check that the common name of the certificate matches the
//	// server host name we connect to.
//	l, err := NewListener("tcp", ":0", th, 0, 0, testUserProvider)
//	if err != nil {
//		t.Fatalf("NewListener failed: %v", err)
//	}
//	defer l.Close()
//
//	// Make sure hostname is added as an entry to /etc/hosts, otherwise ssl handshake will fail
//	host, err := os.Hostname()
//	if err != nil {
//		t.Fatalf("Failed to get os Hostname: %v", err)
//	}
//
//	port := l.Addr().(*net.TCPAddr).Port
//
//	// Create the certs.
//	root, err := ioutil.TempDir("", "TestTLSRequired")
//	if err != nil {
//		t.Fatalf("TempDir failed: %v", err)
//	}
//	defer os.RemoveAll(root)
//
//	// Create the server with TLS config.
//	serverConfig, err := vttls.ServerConfig(
//		path.Join(root, "server-cert.pem"),
//		path.Join(root, "server-key.pem"),
//		path.Join(root, "ca-cert.pem"))
//	if err != nil {
//		t.Fatalf("TLSServerConfig failed: %v", err)
//	}
//	l.TLSConfig.Store(serverConfig)
//	l.RequireSecureTransport = true
//	go l.Accept()
//
//	// Setup conn params without SSL.
//	params := &ConnParams{
//		Host:  host,
//		Port:  port,
//		Uname: "user1",
//		Pass:  "password1",
//	}
//	conn, err := Connect(context.Background(), params)
//	if err == nil {
//		t.Fatal("mysql should have failed")
//	}
//	if conn != nil {
//		conn.Close()
//	}
//
//	// setup conn params with TLS
//	tlstest.CreateSignedCert(root, tlstest.CA, "02", "client", "Client Cert")
//	params.Flags = CapabilityClientSSL
//	params.SslCa = path.Join(root, "ca-cert.pem")
//	params.SslCert = path.Join(root, "client-cert.pem")
//	params.SslKey = path.Join(root, "client-key.pem")
//
//	conn, err = Connect(context.Background(), params)
//	if err != nil {
//		t.Fatalf("mysql failed: %v", err)
//	}
//	if conn != nil {
//		conn.Close()
//	}
//}

func checkCountForTLSVer(t *testing.T, version string, expected int64) {
	connCounts := testCounter.ConnCountByTLSVer
	count, ok := connCounts[version]
	if ok {
		if count != expected {
			t.Errorf("Expected connection count for version %s to be %d, got %d", version, expected, count)
		}
	} else {
		t.Errorf("No count for version %s found in %v", version, connCounts)
	}
}

//func TestErrorCodes(t *testing.T) {
//	th := &testHandler{}
//
//	l, err := newTestListenerDefault()
//	if err != nil {
//		t.Fatalf("NewListener failed: %v", err)
//	}
//	defer l.Close()
//	go l.Accept()
//
//
//	// Setup the right parameters.
//	params := newDefaultConnParam(t, l)
//
//	ctx := context.Background()
//	client, err := Connect(ctx, params)
//	if err != nil {
//		t.Fatalf("error in connect: %v", err)
//	}
//
//	// Test that the right mysql errno/sqlstate are returned for various
//	// internal vitess errors
//	tests := []struct {
//		err      error
//		code     int
//		sqlState string
//		text     string
//	}{
//		{
//			err:      fmt.Errorf("invalid argument"),
//			code:     ERUnknownError,
//			sqlState: SSUnknownSQLState,
//			text:     "invalid argument",
//		},
//		{
//			err: fmt.Errorf(
//				"(errno %v) (sqlstate %v) invalid argument with errno", ERDupEntry, SSDupKey),
//			code:     ERDupEntry,
//			sqlState: SSDupKey,
//			text:     "invalid argument with errno",
//		},
//		{
//			err: fmt.Errorf(
//				"connection deadline exceeded"),
//			code:     ERQueryInterrupted,
//			sqlState: SSUnknownSQLState,
//			text:     "deadline exceeded",
//		},
//		{
//			err: fmt.Errorf(
//				"query pool timeout"),
//			code:     ERTooManyUserConnections,
//			sqlState: SSUnknownSQLState,
//			text:     "resource exhausted",
//		},
//		{
//			err:      util.Wrap(NewSQLError(ERVitessMaxRowsExceeded, SSUnknownSQLState, "Row count exceeded 10000"), "wrapped"),
//			code:     ERVitessMaxRowsExceeded,
//			sqlState: SSUnknownSQLState,
//			text:     "resource exhausted",
//		},
//	}
//
//	for _, test := range tests {
//		th.SetErr(NewSQLErrorFromError(test.err))
//		result, err := client.ExecuteFetch("error", 100, false)
//		if err == nil {
//			t.Fatalf("mysql should have failed but returned: %v", result)
//		}
//		serr, ok := err.(*SQLError)
//		if !ok {
//			t.Fatalf("mysql should have returned a SQLError")
//		}
//
//		if serr.Number() != test.code {
//			t.Errorf("error in %s: want code %v got %v", test.text, test.code, serr.Number())
//		}
//
//		if serr.SQLState() != test.sqlState {
//			t.Errorf("error in %s: want sqlState %v got %v", test.text, test.sqlState, serr.SQLState())
//		}
//
//		if !strings.Contains(serr.Error(), test.err.Error()) {
//			t.Errorf("error in %s: want err %v got %v", test.text, test.err.Error(), serr.Error())
//		}
//	}
//}

const enableCleartextPluginPrefix = "enable-cleartext-plugin: "

// runMysql forks a mysql command line process connecting to the provided server.
func runMysql(t *testing.T, params *ConnParams, command string) (string, bool) {
	output, err := runMysqlWithErr(t, params, command)
	if err != nil {
		return output, false
	}
	return output, true

}
func runMysqlWithErr(t *testing.T, params *ConnParams, command string) (string, error) {
	name := "mysql"
	// The args contain '-v' 3 times, to switch to very verbose output.
	// In particular, it has the message:
	// Query OK, 1 row affected (0.00 sec)
	args := []string{
		"-v", "-v", "-v",
	}
	if strings.HasPrefix(command, enableCleartextPluginPrefix) {
		command = command[len(enableCleartextPluginPrefix):]
		args = append(args, "--enable-cleartext-plugin")
	}
	if command == "--version" {
		args = append(args, command)
	} else {
		args = append(args, "-e", command)
		if params.UnixSocket != "" {
			args = append(args, "-S", params.UnixSocket)
		} else {
			args = append(args,
				"-h", params.Host,
				"-P", fmt.Sprintf("%v", params.Port))
		}
		if params.Uname != "" {
			args = append(args, "-u", params.Uname)
		}
		if params.Pass != "" {
			args = append(args, "-p"+params.Pass)
		}
		if params.DbName != "" {
			args = append(args, "-D", params.DbName)
		}
		if params.Flags&CapabilityClientSSL > 0 {
			args = append(args,
				"--ssl",
				"--ssl-ca", params.SslCa,
				"--ssl-cert", params.SslCert,
				"--ssl-key", params.SslKey,
				"--ssl-verify-server-cert")
		}
	}
	//env := []string{
	//	"LD_LIBRARY_PATH=" + path.Join(dir, "lib/mysql"),
	//}

	t.Logf("Running mysql command: %v %v", name, args)
	cmd := exec.Command(name, args...)
	//cmd.Env = env
	//cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		return output, err
	}
	return output, nil
}

// binaryPath does a limited path lookup for a command,
// searching only within sbin and bin in the given root.
//
// FIXME(alainjobart) move this to vt/env, and use it from
// go/vt/mysqlctl too.
//func binaryPath(root, binary string) (string, error) {
//	subdirs := []string{"sbin", "bin"}
//	for _, subdir := range subdirs {
//		binPath := path.Join(root, subdir, binary)
//		if _, err := os.Stat(binPath); err == nil {
//			return binPath, nil
//		}
//	}
//	return "", fmt.Errorf("%s not found in any of %s/{%s}",
//		binary, root, strings.Join(subdirs, ","))
//}

func TestListenerShutdown(t *testing.T) {
	l, err := newTestListenerDefault()
	if err != nil {
		t.Fatalf("NewListener failed: %v", err)
	}
	defer l.Close()
	go l.Accept()

	// Setup the right parameters.
	params := newDefaultConnParam(t, l)
	initialconnRefuse := testCounter.RefuseCount.Get()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := Connect(ctx, params)
	if err != nil {
		t.Fatalf("Can't connect to listener: %v", err)
	}

	if err := conn.Ping(); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	l.Shutdown()

	if testCounter.RefuseCount.Get()-initialconnRefuse != 1 {
		t.Errorf("Expected connRefuse delta=1, got %d", testCounter.RefuseCount.Get()-initialconnRefuse)
	}

	if err := conn.Ping(); err != nil {
		sqlErr, ok := err.(*SQLError)
		if !ok {
			t.Fatalf("Wrong error type: %T", err)
		}
		if sqlErr.Number() != ERServerShutdown {
			t.Fatalf("Unexpected sql error code: %d", sqlErr.Number())
		}
		if sqlErr.SQLState() != SSServerShutdown {
			t.Fatalf("Unexpected error sql state: %s", sqlErr.SQLState())
		}
		if sqlErr.Message != "Server shutdown in progress" {
			t.Fatalf("Unexpected error message: %s", sqlErr.Message)
		}
	} else {
		t.Fatalf("Ping should fail after shutdown")
	}
}

func TestParseConnAttrs(t *testing.T) {
	expected := map[string]string{
		"_client_version": "8.0.11",
		"program_name":    "mysql",
		"_pid":            "22850",
		"_platform":       "x86_64",
		"_os":             "linux-glibc2.12",
		"_client_name":    "libmysql",
	}

	data := []byte{0x70, 0x04, 0x5f, 0x70, 0x69, 0x64, 0x05, 0x32, 0x32, 0x38, 0x35, 0x30, 0x09, 0x5f, 0x70, 0x6c,
		0x61, 0x74, 0x66, 0x6f, 0x72, 0x6d, 0x06, 0x78, 0x38, 0x36, 0x5f, 0x36, 0x34, 0x03, 0x5f, 0x6f,
		0x73, 0x0f, 0x6c, 0x69, 0x6e, 0x75, 0x78, 0x2d, 0x67, 0x6c, 0x69, 0x62, 0x63, 0x32, 0x2e, 0x31,
		0x32, 0x0c, 0x5f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x08, 0x6c,
		0x69, 0x62, 0x6d, 0x79, 0x73, 0x71, 0x6c, 0x0f, 0x5f, 0x63, 0x6c, 0x69, 0x65, 0x6e, 0x74, 0x5f,
		0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x06, 0x38, 0x2e, 0x30, 0x2e, 0x31, 0x31, 0x0c, 0x70,
		0x72, 0x6f, 0x67, 0x72, 0x61, 0x6d, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x05, 0x6d, 0x79, 0x73, 0x71, 0x6c}

	attrs, pos, err := parseConnAttrs(data, 0)
	if err != nil {
		t.Fatalf("Failed to read connection attributes: %v", err)
	}
	if pos != 113 {
		t.Fatalf("Unexpeded pos after reading connection attributes: %d instead of 113", pos)
	}
	for k, v := range expected {
		if val, ok := attrs[k]; ok {
			if val != v {
				t.Fatalf("Unexpected value found in attrs for key %s: got %s expected %s", k, val, v)
			}
		} else {
			t.Fatalf("Error reading key %s from connection attributes: attrs: %-v", k, attrs)
		}
	}
}

func TestServerFlush(t *testing.T) {
	defer func(saved time.Duration) { *mysqlServerFlushDelay = saved }(*mysqlServerFlushDelay)
	*mysqlServerFlushDelay = 10 * time.Millisecond

	l, err := newTestListenerDefault()
	require.NoError(t, err)
	defer l.Close()
	go l.Accept()

	params := newDefaultConnParam(t, l)

	c, err := Connect(context.Background(), params)
	require.NoError(t, err)
	defer c.Close()

	start := time.Now()
	err = c.ExecuteStreamFetch("50ms delay")
	require.NoError(t, err)

	flds, err := c.Fields()
	require.NoError(t, err)
	if duration, want := time.Since(start), 20*time.Millisecond; duration < *mysqlServerFlushDelay || duration > want {
		t.Errorf("duration: %v, want between %v and %v", duration, *mysqlServerFlushDelay, want)
	}
	want1 := []*types.Field{{
		Name: "result",
		Type: types.VarChar,
	}}
	assert.Equal(t, want1, flds)

	row, err := c.FetchNext()
	require.NoError(t, err)
	if duration, want := time.Since(start), 50*time.Millisecond; duration < want {
		t.Errorf("duration: %v, want > %v", duration, want)
	}
	want2 := []types.Value{types.MakeTrusted(types.VarChar, []byte("delayed"))}
	assert.Equal(t, want2, row)

	row, err = c.FetchNext()
	require.NoError(t, err)
	assert.Nil(t, row)
}
