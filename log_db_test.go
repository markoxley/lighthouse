package lighthouse

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestLogLevelString tests the String() method for all log levels
func TestLogLevelString(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "DEBUG"},
		{LogLevelInfo, "INFO"},
		{LogLevelWarn, "WARN"},
		{LogLevelError, "ERROR"},
		{LogLevelPanic, "PANIC"},
		{LogLevelFatal, "FATAL"},
		{LogLevel(99), "UNKNOWN"},
		{LogLevel(-1), "UNKNOWN"},
	}

	for _, tt := range tests {
		result := tt.level.String()
		if result != tt.expected {
			t.Errorf("LogLevel(%d).String() = %q, expected %q", tt.level, result, tt.expected)
		}
	}
}

// TestLogDBConstants verifies LogDB type constants
func TestLogDBConstants(t *testing.T) {
	tests := []struct {
		dbType   LogDB
		expected int
	}{
		{LogDBSQLite, 0},
		{LogDBPostgreSQL, 1},
		{LogDBMySQL, 2},
		{LogDBMSSQL, 3},
		{LogDBOracle, 4},
	}

	for _, tt := range tests {
		if int(tt.dbType) != tt.expected {
			t.Errorf("LogDB %d expected %d, got %d", tt.dbType, tt.expected, int(tt.dbType))
		}
	}
}

// TestGetTableCreationSQLite tests getTableCreation for SQLite
func TestGetTableCreationSQLite(t *testing.T) {
	tbl, idx, ins, err := getTableCreation(LogDBSQLite)
	if err != nil {
		t.Errorf("Unexpected error for SQLite: %v", err)
	}
	if !strings.Contains(tbl, "CREATE TABLE") {
		t.Error("SQLite table query should contain CREATE TABLE")
	}
	if !strings.Contains(idx, "CREATE INDEX") {
		t.Error("SQLite index query should contain CREATE INDEX")
	}
	if !strings.Contains(ins, "INSERT INTO") {
		t.Error("SQLite insert query should contain INSERT INTO")
	}
	_ = idx
}

// TestGetTableCreationPostgreSQL tests getTableCreation for PostgreSQL
func TestGetTableCreationPostgreSQL(t *testing.T) {
	tbl, _, ins, err := getTableCreation(LogDBPostgreSQL)
	if err != nil {
		t.Errorf("Unexpected error for PostgreSQL: %v", err)
	}
	if !strings.Contains(tbl, "BIGINT") {
		t.Error("PostgreSQL table should use BIGINT")
	}
	if !strings.Contains(ins, "$1") {
		t.Error("PostgreSQL insert should use $N placeholders")
	}
}

// TestGetTableCreationMySQL tests getTableCreation for MySQL
func TestGetTableCreationMySQL(t *testing.T) {
	tbl, _, ins, err := getTableCreation(LogDBMySQL)
	if err != nil {
		t.Errorf("Unexpected error for MySQL: %v", err)
	}
	if !strings.Contains(tbl, "BIGINT") {
		t.Error("MySQL table should use BIGINT")
	}
	if !strings.Contains(ins, "VALUES (?,") {
		t.Error("MySQL insert should use ? placeholders")
	}
}

// TestGetTableCreationMSSQL tests getTableCreation for MSSQL
func TestGetTableCreationMSSQL(t *testing.T) {
	tbl, _, ins, err := getTableCreation(LogDBMSSQL)
	if err != nil {
		t.Errorf("Unexpected error for MSSQL: %v", err)
	}
	if !strings.Contains(tbl, "sys.objects") {
		t.Error("MSSQL table query should reference sys.objects")
	}
	if !strings.Contains(ins, "@p1") {
		t.Error("MSSQL insert should use @pN placeholders")
	}
}

// TestGetTableCreationOracle tests getTableCreation for Oracle
func TestGetTableCreationOracle(t *testing.T) {
	tbl, _, ins, err := getTableCreation(LogDBOracle)
	if err != nil {
		t.Errorf("Unexpected error for Oracle: %v", err)
	}
	if !strings.Contains(tbl, "NUMBER") {
		t.Error("Oracle table should use NUMBER types")
	}
	if !strings.Contains(ins, ":1") {
		t.Error("Oracle insert should use :N placeholders")
	}
}

// TestGetTableCreationInvalid tests getTableCreation with invalid type
func TestGetTableCreationInvalid(t *testing.T) {
	_, _, _, err := getTableCreation(LogDB(99))
	if err == nil {
		t.Error("Expected error for invalid database type")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Error("Error message should mention 'unsupported'")
	}
}

// TestSanitizeTableName tests table name sanitization
func TestSanitizeTableName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"logs", "logs"},
		{"app_logs", "app_logs"},
		{"AppLogs123", "AppLogs123"},
		{`logs"table`, "logstable"},
		{`'logs'`, "logs"},
		{"logs; DROP TABLE users;--", "logsDROPTABLEusers"},
		{"logs table", "logstable"},
		{"logs-table", "logstable"},
		{"logs.table", "logstable"},
		{"logs/table", "logstable"},
		{"logs@table", "logstable"},
		{"logs#table", "logstable"},
		{"", ""},
		{"123logs", "123logs"},
		{"__logs__", "__logs__"},
	}

	for _, tt := range tests {
		result := sanitizeTableName(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeTableName(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

// TestSanitizeTableNameUnicode tests that unicode is handled
func TestSanitizeTableNameUnicode(t *testing.T) {
	// Unicode characters should be stripped
	result := sanitizeTableName("logs_日本語")
	if result != "logs_" {
		t.Errorf("Expected unicode to be stripped, got %q", result)
	}
}

// TestSetDatabaseSQLite tests SetDatabase with SQLite
func TestSetDatabaseSQLite(t *testing.T) {
	originalLogger := logger
	defer func() { logger = originalLogger }()

	// Create a test in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	testLogger := &Logger{
		logLevel: LogLevelInfo,
		buffer:   make(chan logObject, 100),
	}
	logger = testLogger

	err = SetDatabase(LogDBSQLite, db, "test_logs")
	if err != nil {
		t.Errorf("SetDatabase failed: %v", err)
	}

	// Verify the table was created
	var count int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='test_logs'").Scan(&count)
	if err != nil {
		t.Errorf("Failed to check table existence: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected table to be created, count=%d", count)
	}

	// Verify insertString was set
	if logger.insertString == "" {
		t.Error("insertString should be set after SetDatabase")
	}
}

// TestSetDatabaseDefaultTableName tests SetDatabase with default table name
func TestSetDatabaseDefaultTableName(t *testing.T) {
	originalLogger := logger
	defer func() { logger = originalLogger }()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	testLogger := &Logger{
		logLevel: LogLevelInfo,
		buffer:   make(chan logObject, 100),
	}
	logger = testLogger

	// No table name provided - should use default "logs"
	err = SetDatabase(LogDBSQLite, db)
	if err != nil {
		t.Errorf("SetDatabase failed: %v", err)
	}

	// Verify the default table was created
	var count int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='logs'").Scan(&count)
	if err != nil {
		t.Errorf("Failed to check table existence: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected default table 'logs' to be created, count=%d", count)
	}
}

// TestSetDatabaseInvalidDBType tests SetDatabase with invalid DB type
func TestSetDatabaseInvalidDBType(t *testing.T) {
	originalLogger := logger
	defer func() { logger = originalLogger }()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	testLogger := &Logger{
		logLevel: LogLevelInfo,
		buffer:   make(chan logObject, 100),
	}
	logger = testLogger

	err = SetDatabase(LogDB(999), db)
	if err == nil {
		t.Error("Expected error for invalid database type")
	}
}

// TestSetDatabaseNilDB tests SetDatabase with nil DB
func TestSetDatabaseNilDB(t *testing.T) {
	originalLogger := logger
	defer func() { logger = originalLogger }()

	testLogger := &Logger{
		logLevel: LogLevelInfo,
		buffer:   make(chan logObject, 100),
	}
	logger = testLogger

	err := SetDatabase(LogDBSQLite, nil)
	if err == nil {
		t.Error("Expected error for nil database")
	}
	if !strings.Contains(err.Error(), "cannot be nil") {
		t.Errorf("Expected error message to contain 'cannot be nil', got: %v", err)
	}
}

// TestWriteWithDatabase tests that write() correctly inserts to database
func TestWriteWithDatabase(t *testing.T) {
	originalLogger := logger
	defer func() { logger = originalLogger }()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	testLogger := &Logger{
		logLevel: LogLevelDebug,
		buffer:   make(chan logObject, 100),
	}
	logger = testLogger

	// Set up database
	err = SetDatabase(LogDBSQLite, db, "test_logs")
	if err != nil {
		t.Fatalf("SetDatabase failed: %v", err)
	}

	// Write a log directly (bypassing buffer)
	testTime := time.Now()
	testLogger.write(LogLevelInfo, testTime, "Test DB message", map[string]string{"key": "value"})

	// Give it a moment to insert
	time.Sleep(100 * time.Millisecond)

	// Query the database to verify insertion
	var timestamp int64
	var level int
	var levelDesc string
	var message string
	var params string

	err = db.QueryRow("SELECT timeStamp, level, levelDescription, message, params FROM test_logs LIMIT 1").Scan(
		&timestamp, &level, &levelDesc, &message, &params)
	if err != nil {
		t.Errorf("Failed to query log from database: %v", err)
		return
	}

	if message != "Test DB message" {
		t.Errorf("Expected message 'Test DB message', got '%s'", message)
	}
	if level != int(LogLevelInfo) {
		t.Errorf("Expected level %d, got %d", LogLevelInfo, level)
	}
	if levelDesc != "INFO" {
		t.Errorf("Expected levelDescription 'INFO', got '%s'", levelDesc)
	}
}

// TestDatabaseQueryConstants tests that all query constants are properly formatted
func TestDatabaseQueryConstants(t *testing.T) {
	// Verify that the format strings expect exactly one %[1]s argument
	tests := []struct {
		name  string
		query string
	}{
		{"sqliteCreateTableQuery", sqliteCreateTableQuery},
		{"sqliteCreateIndexQuery", sqliteCreateIndexQuery},
		{"sqliteInsertQuery", sqliteInsertQuery},
		{"postgreSQLCreateTableQuery", postgreSQLCreateTableQuery},
		{"postgreSQLCreateIndexQuery", postgreSQLCreateIndexQuery},
		{"postgreSQLInsertQuery", postgreSQLInsertQuery},
		{"mysqlCreateTableQuery", mysqlCreateTableQuery},
		{"mysqlCreateIndexQuery", mysqlCreateIndexQuery},
		{"mysqlInsertQuery", mysqlInsertQuery},
		{"mssqlCreateTableQuery", mssqlCreateTableQuery},
		{"mssqlCreateIndexQuery", mssqlCreateIndexQuery},
		{"mssqlInsertQuery", mssqlInsertQuery},
		{"oracleCreateTableQuery", oracleCreateTableQuery},
		{"oracleCreateIndexQuery", oracleCreateIndexQuery},
		{"oracleInsertQuery", oracleInsertQuery},
	}

	for _, tt := range tests {
		// Verify the format string can be formatted
		result := fmt.Sprintf(tt.query, "test_table")
		if !strings.Contains(result, "test_table") {
			t.Errorf("%s: formatted query should contain table name", tt.name)
		}
	}
}

// TestWriteWithNilParams tests database insertion with nil params
func TestWriteWithNilParams(t *testing.T) {
	originalLogger := logger
	defer func() { logger = originalLogger }()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	testLogger := &Logger{
		logLevel: LogLevelDebug,
		buffer:   make(chan logObject, 100),
	}
	logger = testLogger

	err = SetDatabase(LogDBSQLite, db, "test_logs")
	if err != nil {
		t.Fatalf("SetDatabase failed: %v", err)
	}

	// Write with nil params
	testLogger.write(LogLevelError, time.Now(), "Error without params", nil)

	time.Sleep(100 * time.Millisecond)

	var count int
	err = db.QueryRow("SELECT count(*) FROM test_logs").Scan(&count)
	if err != nil {
		t.Errorf("Failed to count logs: %v", err)
		return
	}
	if count != 1 {
		t.Errorf("Expected 1 log entry, got %d", count)
	}
}

// TestWriteFiltersBelowLevelWithDB tests that filtered logs don't hit database
func TestWriteFiltersBelowLevelWithDB(t *testing.T) {
	originalLogger := logger
	defer func() { logger = originalLogger }()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	testLogger := &Logger{
		logLevel: LogLevelError, // Only Error and above
		buffer:   make(chan logObject, 100),
	}
	logger = testLogger

	err = SetDatabase(LogDBSQLite, db, "test_logs")
	if err != nil {
		t.Fatalf("SetDatabase failed: %v", err)
	}

	// These should be filtered
	testLogger.write(LogLevelDebug, time.Now(), "Debug msg", nil)
	testLogger.write(LogLevelInfo, time.Now(), "Info msg", nil)
	testLogger.write(LogLevelWarn, time.Now(), "Warn msg", nil)

	// This should go through
	testLogger.write(LogLevelError, time.Now(), "Error msg", nil)

	time.Sleep(100 * time.Millisecond)

	var count int
	err = db.QueryRow("SELECT count(*) FROM test_logs").Scan(&count)
	if err != nil {
		t.Errorf("Failed to count logs: %v", err)
		return
	}
	if count != 1 {
		t.Errorf("Expected only 1 log entry (Error), got %d", count)
	}
}

// TestConstantsNoDuplicates verifies no constant is accidentally redefined
func TestConstantsNoDuplicates(t *testing.T) {
	// This test documents which file contains which constants
	// LogLevel - defined in db_constants.go
	// LogDB - defined in db_constants.go
	// ansi colors - defined in db_constants.go
	// bufferSize - defined in db_constants.go

	// Verify values are as expected from db_constants.go
	if bufferSize != 10000 {
		t.Errorf("bufferSize expected 10000, got %d", bufferSize)
	}
	if defaultTableName != "logs" {
		t.Errorf("defaultTableName expected 'logs', got %s", defaultTableName)
	}
}

// BenchmarkSanitizeTableName benchmarks the sanitize function
func BenchmarkSanitizeTableName(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sanitizeTableName("logs; DROP TABLE users;--")
	}
}
