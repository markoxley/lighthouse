// Package lighthouse - Database Constants and Type Definitions
//
// This file contains all the database-related constants and type definitions
// used by the lighthouse logging package.
//
// Author: Mark Oxley
//
// The SQL queries are formatted using Go's printf-style formatting with %[1]s
// representing the table name placeholder.
package lighthouse

// Database creation and insertion queries for each supported database type.
// These constants use Go format strings where %[1]s is replaced with the table name.
const (
	// SQLite queries use INTEGER for timestamps and ? placeholders
	sqliteCreateTableQuery = "CREATE TABLE IF NOT EXISTS %[1]s (timeStamp INTEGER, level INTEGER, levelDescription TEXT, message TEXT, params TEXT)"
	sqliteCreateIndexQuery = "CREATE INDEX IF NOT EXISTS idx_%[1]s_ts ON %[1]s (timeStamp)"
	sqliteInsertQuery      = "INSERT INTO %[1]s (timeStamp, level, levelDescription, message, params) VALUES (?, ?, ?, ?, ?)"

	// PostgreSQL queries use BIGINT for timestamps and $N placeholders
	postgreSQLCreateTableQuery = "CREATE TABLE IF NOT EXISTS %[1]s (timeStamp BIGINT, level INTEGER, levelDescription TEXT, message TEXT, params TEXT)"
	postgreSQLCreateIndexQuery = "CREATE INDEX IF NOT EXISTS idx_%[1]s_ts ON %[1]s (timeStamp)"
	postgreSQLInsertQuery      = "INSERT INTO %[1]s (timeStamp, level, levelDescription, message, params) VALUES ($1, $2, $3, $4, $5)"

	// MySQL queries use BIGINT for timestamps and ? placeholders
	mysqlCreateTableQuery = "CREATE TABLE IF NOT EXISTS %[1]s (timeStamp BIGINT, level INT, levelDescription TEXT, message TEXT, params TEXT)"
	mysqlCreateIndexQuery = "CREATE INDEX idx_%[1]s_ts ON %[1]s (timeStamp) /*!50100 IF NOT EXISTS */"
	mysqlInsertQuery      = "INSERT INTO %[1]s (timeStamp, level, levelDescription, message, params) VALUES (?, ?, ?, ?, ?)"

	// MSSQL queries use BIGINT for timestamps and @pN placeholders
	mssqlCreateTableQuery = "IF NOT EXISTS (SELECT * FROM sys.objects WHERE object_id = OBJECT_ID(N'%[1]s') AND type in (N'U')) BEGIN CREATE TABLE %[1]s (timeStamp BIGINT, level INT, levelDescription NVARCHAR(MAX), message NVARCHAR(MAX), params NVARCHAR(MAX)) END"
	mssqlCreateIndexQuery = "IF NOT EXISTS (SELECT * FROM sys.indexes WHERE object_id = OBJECT_ID(N'%[1]s') AND name = 'idx_%[1]s_ts') BEGIN CREATE INDEX idx_%[1]s_ts ON %[1]s (timeStamp) END"
	mssqlInsertQuery      = "INSERT INTO %[1]s (timeStamp, level, levelDescription, message, params) VALUES (@p1, @p2, @p3, @p4, @p5)"

	// Oracle queries use NUMBER for timestamps and :N placeholders
	oracleCreateTableQuery = "BEGIN EXECUTE IMMEDIATE 'CREATE TABLE %[1]s (timeStamp NUMBER(19), level NUMBER(10), levelDescription CLOB, message CLOB, params CLOB)'; EXCEPTION WHEN OTHERS THEN IF SQLCODE = -955 THEN NULL; ELSE RAISE; END IF; END;"
	oracleCreateIndexQuery = "DECLARE already_exists EXCEPTION; PRAGMA EXCEPTION_INIT(already_exists, -955); BEGIN EXECUTE IMMEDIATE 'CREATE INDEX idx_%[1]s_ts ON %[1]s (timeStamp)'; EXCEPTION WHEN already_exists THEN NULL; WHEN OTHERS THEN RAISE; END;"
	oracleInsertQuery      = "INSERT INTO %[1]s (timeStamp, level, levelDescription, message, params) VALUES (:1, :2, :3, :4, :5)"
)

// LogLevel constants define the severity levels for log entries.
// The iota enumeration ensures proper ordering for level comparisons.
// Higher values indicate more severe log levels.
const (
	LogLevelDebug LogLevel = iota // Debug: Detailed troubleshooting information
	LogLevelInfo                  // Info: General operational status
	LogLevelWarn                  // Warn: Potential issues that don't prevent operation
	LogLevelError                 // Error: Failures that prevented an operation
	LogLevelPanic                 // Panic: Critical failures that crash the application
	LogLevelFatal                 // Fatal: Critical errors that terminate the application
)

// LogDB constants identify the supported database types.
// Use these with SetDatabase() to configure persistence.
const (
	LogDBSQLite     LogDB = iota // SQLite (file-based, serverless)
	LogDBPostgreSQL              // PostgreSQL
	LogDBMySQL                   // MySQL / MariaDB
	LogDBMSSQL                   // Microsoft SQL Server
	LogDBOracle                  // Oracle Database
)

// ANSI color codes for console output.
// These codes work on most Unix-like terminals and Windows 10+.
const (
	ansiEscapePrefix = "\033["                  // Start of ANSI escape sequence
	Red              = ansiEscapePrefix + "31m" // Red text
	Green            = ansiEscapePrefix + "32m" // Green text
	Yellow           = ansiEscapePrefix + "33m" // Yellow text
	Blue             = ansiEscapePrefix + "34m" // Blue text
	Magenta          = ansiEscapePrefix + "35m" // Magenta text
	Cyan             = ansiEscapePrefix + "36m" // Cyan text
	White            = ansiEscapePrefix + "37m" // White text
	ansiReset        = "\033[0m"                // Reset to default color
)

// defaultTableName is the default name for the log table in databases.
// It can be overridden by passing a custom name to SetDatabase().
const defaultTableName = "logs"

// bufferSize defines the capacity of the log entry channel.
// A larger buffer reduces blocking but uses more memory.
// Default is 10000 entries which provides good throughput for most applications.
const bufferSize = 10000
