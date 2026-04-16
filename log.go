// Package lighthouse provides a high-performance, thread-safe logging module for Go applications.
//
// Author: Mark Oxley
//
// Features:
//   - Asynchronous buffered logging for high throughput
//   - Thread-safe concurrent operations
//   - Multiple log levels (Debug, Info, Warn, Error, Panic, Fatal)
//   - Colored console output with ANSI escape codes
//   - Structured logging with template substitution (e.g., "Hello {name}")
//   - External log function support for custom handlers (databases, APIs, etc.)
//   - Database persistence support (SQLite, PostgreSQL, MySQL, MSSQL, Oracle)
//   - Graceful shutdown with signal handling (SIGTERM, SIGINT)
//   - io.Writer interface compatibility for standard library integration
//
// Basic Usage:
//
//	import "github.com/markoxley/lighthouse"
//
//	func main() {
//	    lighthouse.Info("Application started")
//	    lighthouse.Debug("Debug info: {value}", 42)
//	    lighthouse.Error("An error occurred: {err}", err)
//	}
//
// Database Logging:
//
//	db, _ := sql.Open("sqlite3", "app.db")
//	lighthouse.SetDatabase(lighthouse.LogDBSQLite, db, "logs")
//
// The logger uses a buffered channel (default size: 10000) to queue log entries,
// ensuring minimal latency for the calling code. A dedicated goroutine processes
// the buffer asynchronously, writing to console, external handlers, and databases.
//
// For Fatal and Panic levels, the logger ensures logs are flushed before termination.
package lighthouse

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LogLevel represents the severity level of a log entry.
// Levels are ordered from least to most severe: Debug < Info < Warn < Error < Panic < Fatal
type LogLevel int

// LogFunc is a callback function type for custom log handlers.
// It receives the log level, timestamp, message, and structured parameters.
// This allows integration with external systems like databases, monitoring services, or APIs.
type LogFunc func(level LogLevel, dt time.Time, message string, params map[string]string)

// LogDB identifies the database type for persistence.
// Supported databases: SQLite, PostgreSQL, MySQL, Microsoft SQL Server, Oracle
type LogDB int

// String returns the string representation of a LogLevel.
// Returns "UNKNOWN" for undefined levels.
func (ll LogLevel) String() string {
	switch ll {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelPanic:
		return "PANIC"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Global package-level variables
var (
	// logger is the singleton Logger instance used by all package-level functions.
	// It is initialized in the init() function with default settings.
	logger *Logger

	// DefaultLogger provides access to the global logger instance for advanced use cases.
	// This allows direct access to the underlying Logger methods.
	DefaultLogger *Logger

	// pfxs maps string prefixes to their corresponding LogLevel values.
	// Used by the Write method to parse log levels from standard library formatted messages.
	pfxs = map[string]LogLevel{
		LogLevelDebug.String(): LogLevelDebug,
		LogLevelInfo.String():  LogLevelInfo,
		LogLevelWarn.String():  LogLevelWarn,
		LogLevelError.String(): LogLevelError,
		LogLevelPanic.String(): LogLevelPanic,
		LogLevelFatal.String(): LogLevelFatal,
	}
	// logColours maps each LogLevel to its corresponding ANSI color code.
	// These colors are used for console output to improve readability.
	logColours = map[LogLevel]string{
		LogLevelDebug: Blue,    // Blue for debug messages
		LogLevelInfo:  Green,   // Green for informational messages
		LogLevelWarn:  Yellow,  // Yellow for warnings
		LogLevelError: Red,     // Red for errors
		LogLevelPanic: Magenta, // Magenta for panics
		LogLevelFatal: Red,     // Red for fatal errors
	}
)

// logObject represents a single log entry in the buffer.
// It holds all the information needed to process and output a log message.
type logObject struct {
	level   LogLevel          // Severity level of this log entry
	dt      time.Time         // Timestamp when the log was created
	message string            // The formatted log message
	params  map[string]string // Structured parameters extracted from template placeholders
}

// Logger is the core logging structure that handles all log operations.
// It implements io.Writer for compatibility with the standard library log package.
// The Logger uses a buffered channel for asynchronous processing and is safe for concurrent use.
type Logger struct {
	logLevel     LogLevel       // Current minimum log level (messages below this are filtered)
	logFunc      LogFunc        // Optional external handler for custom log processing
	mu           sync.Mutex     // Protects shared state (logLevel, logFunc, db) from concurrent access
	buffer       chan logObject // Buffered channel for asynchronous log processing
	wg           sync.WaitGroup // Used to wait for buffer processing on shutdown
	db           *sql.DB        // Database connection for persistence (optional)
	dbType       LogDB          // Type of database being used
	insertString string         // Pre-formatted SQL INSERT statement for database logging
	started      bool           // Whether the background goroutine has been started
}

// Write implements the io.Writer interface, allowing Logger to be used with the standard library log package.
// It parses the input to extract timestamp and log level from standard formatted messages.
// The format "2006/01/02 15:04:05 LEVEL message" is recognized and parsed appropriately.
//
// This method enables integration like: log.SetOutput(lighthouse.Logger)
func (l *Logger) Write(p []byte) (n int, err error) {
	lvl := LogLevelInfo
	m := string(p)
	dt := time.Now()
	if len(p) >= 20 {
		dt, err = time.Parse("2006/01/02 15:04:05", string(p[:19]))
		if err != nil {
			dt = time.Now()
		} else {
			m = string(p[20:])
		}
	}
	for k, v := range pfxs {
		if strings.HasPrefix(m, k) {
			lvl = v
			break
		}
	}
	l.write(lvl, dt, m, nil)
	return len(p), nil
}

// init initializes the package-level logger singleton.
// It sets up:
//   - A Logger instance with default Info level and buffered channel
//   - Signal handling for graceful shutdown on SIGINT/SIGTERM
//   - The background buffer processing goroutine
//   - Integration with the standard library log package
//
// This function is called automatically when the package is imported.
// init initializes the package-level logger singleton.
// It sets up:
//   - A Logger instance with default Info level and buffered channel
//   - Signal handling for graceful shutdown on SIGINT/SIGTERM
//   - The background buffer processing goroutine
//   - Integration with the standard library log package
//
// This function is called automatically when the package is imported.
func init() {
	// Create the singleton logger with default configuration
	logger = &Logger{
		logLevel: LogLevelInfo,                     // Default to Info level (Debug messages filtered)
		buffer:   make(chan logObject, bufferSize), // Buffered channel prevents blocking callers
	}
	DefaultLogger = logger

	// Start the background goroutine that processes the log buffer
	logger.Start()

	// Redirect standard library log output to our Logger
	// This captures logs from third-party libraries using the standard log package
	log.SetOutput(logger)

	// Set up signal handling for graceful shutdown
	// When SIGINT (Ctrl+C) or SIGTERM is received, flush the buffer before exiting
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		flush()
		os.Exit(0)
	}()
}

// SetOutput sets a custom handler function for the global logger.
// The handler receives structured log data and can implement custom logic
// such as sending logs to external services, databases, or monitoring systems.
// This operation is thread-safe.
//
// Example:
//
//	lighthouse.SetOutput(func(level lighthouse.LogLevel, dt time.Time, message string, params map[string]string) {
//	    // Send to monitoring service
//	    monitoringClient.Log(level.String(), message)
//	})
func SetOutput(logFunc LogFunc) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.logFunc = logFunc
}

// SetOutput sets a custom handler function for this logger instance.
// The handler receives structured log data and can implement custom logic
// such as sending logs to external services, databases, or monitoring systems.
// This operation is thread-safe.
func (l *Logger) SetOutput(logFunc LogFunc) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logFunc = logFunc
}

// NewLogger creates a new Logger instance with the specified log level.
// This allows creating multiple independent loggers for different components or for testing.
// The returned logger must be started with Start() to begin processing log entries.
//
// Example:
//
//	logger := lighthouse.NewLogger(lighthouse.LogLevelDebug)
//	logger.Start()
//	defer logger.Stop()
//	logger.Info("Component started")
func NewLogger(level LogLevel) *Logger {
	return &Logger{
		logLevel: level,
		buffer:   make(chan logObject, bufferSize),
	}
}

// Start begins the background goroutine that processes the log buffer.
// This must be called before any logging methods are used.
// It is safe to call multiple times (subsequent calls are no-ops).
func (l *Logger) Start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return
	}
	l.wg.Add(1)
	go l.processBuffer()
	l.started = true
}

// Stop closes the log buffer and waits for all pending logs to be processed.
// This ensures no logs are lost during graceful shutdown.
// After calling Stop, no new logs can be written to this logger.
func (l *Logger) Stop() {
	l.mu.Lock()
	if !l.started {
		l.mu.Unlock()
		return
	}
	l.started = false
	l.mu.Unlock()
	close(l.buffer)
	l.wg.Wait()
}

// SetLevel sets the minimum log level for the global logger.
// Messages with a level lower than this will be silently discarded.
// This operation is thread-safe.
//
// Example:
//
//	lighthouse.SetLevel(lighthouse.LogLevelWarn)  // Only Warn, Error, Panic, Fatal will be logged
func SetLevel(level LogLevel) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.logLevel = level
}

// SetLevel sets the minimum log level for this logger instance.
// Messages with a level lower than this will be silently discarded.
// This operation is thread-safe.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logLevel = level
}

// SetDatabase configures database persistence for the global logger.
// It automatically creates the required table and index if they don't exist.
// The table name is sanitized to prevent SQL injection.
//
// Parameters:
//   - dbType: The database type (LogDBSQLite, LogDBPostgreSQL, LogDBMySQL, LogDBMSSQL, LogDBOracle)
//   - db: An initialized *sql.DB connection
//   - table: Optional custom table name (defaults to "logs")
//
// Example:
//
//	db, _ := sql.Open("sqlite3", "app.db")
//	err := lighthouse.SetDatabase(lighthouse.LogDBSQLite, db, "application_logs")
func SetDatabase(dbType LogDB, db *sql.DB, table ...string) error {
	if db == nil {
		return fmt.Errorf("database connection cannot be nil")
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.db = db
	tableName := defaultTableName
	if len(table) > 0 {
		tableName = table[0]
	}
	tableName = sanitizeTableName(tableName)
	tblQuery, idxQuery, insertQuery, err := getTableCreation(dbType)
	if err != nil {
		return err
	}
	_, err = logger.db.Exec(fmt.Sprintf(tblQuery, tableName))
	if err != nil {
		return fmt.Errorf("failed to create table %s: %s", tableName, err.Error())
	}
	_, err = logger.db.Exec(fmt.Sprintf(idxQuery, tableName))
	if err != nil {
		return fmt.Errorf("failed to create index on table %s: %s", tableName, err.Error())
	}
	logger.insertString = fmt.Sprintf(insertQuery, tableName)
	return nil
}

// SetDatabase configures database persistence for this logger instance.
// It automatically creates the required table and index if they don't exist.
// The table name is sanitized to prevent SQL injection.
//
// Parameters:
//   - dbType: The database type (LogDBSQLite, LogDBPostgreSQL, LogDBMySQL, LogDBMSSQL, LogDBOracle)
//   - db: An initialized *sql.DB connection
//   - table: Optional custom table name (defaults to "logs")
func (l *Logger) SetDatabase(dbType LogDB, db *sql.DB, table ...string) error {
	if db == nil {
		return fmt.Errorf("database connection cannot be nil")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.db = db
	tableName := defaultTableName
	if len(table) > 0 {
		tableName = table[0]
	}
	tableName = sanitizeTableName(tableName)
	tblQuery, idxQuery, insertQuery, err := getTableCreation(dbType)
	if err != nil {
		return err
	}
	_, err = l.db.Exec(fmt.Sprintf(tblQuery, tableName))
	if err != nil {
		return fmt.Errorf("failed to create table %s: %s", tableName, err.Error())
	}
	_, err = l.db.Exec(fmt.Sprintf(idxQuery, tableName))
	if err != nil {
		return fmt.Errorf("failed to create index on table %s: %s", tableName, err.Error())
	}
	l.insertString = fmt.Sprintf(insertQuery, tableName)
	return nil
}

// processBuffer is the background goroutine that processes log entries from the buffer.
// It runs continuously until the buffer channel is closed (during shutdown).
// For each log entry, it:
//   - Writes to console with appropriate colors
//   - Calls the external log function if configured
//   - Persists to database if configured
//   - Handles Fatal (os.Exit) and Panic by terminating appropriately after logging
//
// This method should be called as a goroutine: go logger.processBuffer()
func (l *Logger) processBuffer() {
	defer l.wg.Done() // Signal completion when the goroutine exits

	// Range over the buffer channel until it's closed
	for logObj := range l.buffer {
		// Process the log entry (console, custom handler, database)
		l.write(logObj.level, logObj.dt, logObj.message, logObj.params)

		// For Fatal and Panic levels, terminate after logging
		if logObj.level >= LogLevelFatal {
			os.Exit(1) // Fatal errors terminate with exit code 1
		}
		if logObj.level >= LogLevelPanic {
			panic(logObj.message) // Panic causes a stack trace
		}
	}
}

// write outputs a log entry to all configured destinations.
// It handles console output with ANSI colors, custom log functions, and database persistence.
// This method is called internally by processBuffer and is protected by a mutex for thread safety.
// Messages below the current log level are silently discarded.
func (l *Logger) write(level LogLevel, dt time.Time, message string, params map[string]string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if level < l.logLevel {
		return
	}
	col := logColours[level]
	for strings.HasSuffix(message, "\n") {
		message = message[:len(message)-1]
	}

	if l.logFunc != nil {
		l.logFunc(level, dt, message, params)
	} else {
		fmt.Printf("%s %s%s%s\n", dt.Format("2006/01/02 15:04:05"), col, message, ansiReset)
	}
	if l.db != nil && l.insertString != "" {
		paramsJSON, _ := json.Marshal(params)
		_, err := l.db.Exec(l.insertString, dt.Unix(), level, level.String(), message, string(paramsJSON))
		if err != nil {
			// Log database errors to console as a fallback
			fmt.Printf("%s %s%s%s [DB ERROR: %v]\n", dt.Format("2006/01/02 15:04:05"), Red, err.Error(), ansiReset, err)
		}
	}
}

// parseMessage parses a template message with placeholders like "Hello {name}".
// It replaces placeholders with provided arguments and extracts structured parameters.
//
// Placeholder syntax:
//   - {key} - Named placeholder, will be replaced with corresponding argument
//   - Supports escape sequences: \{ and \} for literal braces
//
// Parameters:
//   - message: Template string with {placeholders}
//   - args: Values to substitute into placeholders (in order of appearance)
//
// Returns:
//   - The message with placeholders replaced
//   - A map of parameter names to values for structured logging
//
// Example:
//
//	msg, params := lighthouse.parseMessage("Hello {name}, you have {count} messages", "Alice", "5")
//	// msg = "Hello Alice, you have 5 messages"
//	// params = {"name": "Alice", "count": "5"}
func parseMessage(message string, args ...interface{}) (string, map[string]string) {
	if len(args) == 0 {
		return message, nil
	}
	keys := make([]string, 0, len(args))
	current := ""
	slash := false
	inKey := false
	for _, c := range message {
		if c == '\\' && !slash {
			slash = true
			continue
		}
		if c == '{' && !slash && !inKey {
			current = ""
			inKey = true
			continue
		}
		if c == '}' && !slash && inKey {
			keys = append(keys, current)
			current = ""
			inKey = false
			continue
		}
		if inKey {
			current += string(c)
		}
		slash = false
	}
	result := make(map[string]string)
	for i, k := range keys {
		if _, exists := result[k]; exists {
			// Skip duplicate keys - they already have a value
			continue
		}
		if i < len(args) {
			result[k] = fmt.Sprintf("%v", args[i])
		} else {
			result[k] = ""
		}
	}
	for _, k := range keys {
		message = strings.ReplaceAll(message, "{"+k+"}", result[k])
	}
	return message, result
}

// Debug logs a message at the Debug level.
// Debug messages are useful for development and troubleshooting.
// They are filtered out when the log level is set to Info or higher.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
//
// Example:
//
//	lighthouse.Debug("Processing item {id}", itemID)
func Debug(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelDebug, dt: time.Now(), message: msg, params: params}

	select {
	case logger.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		logger.write(LogLevelDebug, time.Now(), msg, params)
	}
}

// Debug logs a message at the Debug level on this logger instance.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
func (l *Logger) Debug(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelDebug, dt: time.Now(), message: msg, params: params}

	select {
	case l.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		l.write(LogLevelDebug, time.Now(), msg, params)
	}
}

// Info logs a message at the Info level.
// Info messages are general application status messages.
// They are useful for tracking normal application flow.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
//
// Example:
//
//	lighthouse.Info("Server started on port {port}", 8080)
func Info(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelInfo, dt: time.Now(), message: msg, params: params}

	select {
	case logger.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		logger.write(LogLevelInfo, time.Now(), msg, params)
	}
}

// Info logs a message at the Info level on this logger instance.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
func (l *Logger) Info(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelInfo, dt: time.Now(), message: msg, params: params}

	select {
	case l.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		l.write(LogLevelInfo, time.Now(), msg, params)
	}
}

// Warn logs a message at the Warn level.
// Warning messages indicate potentially harmful situations that don't prevent
// the application from continuing but should be investigated.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
//
// Example:
//
//	lighthouse.Warn("Connection pool at {percent}% capacity", 85)
func Warn(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelWarn, dt: time.Now(), message: msg, params: params}

	select {
	case logger.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		logger.write(LogLevelWarn, time.Now(), msg, params)
	}
}

// Warn logs a message at the Warn level on this logger instance.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
func (l *Logger) Warn(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelWarn, dt: time.Now(), message: msg, params: params}

	select {
	case l.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		l.write(LogLevelWarn, time.Now(), msg, params)
	}
}

// Error logs a message at the Error level.
// Error messages indicate failures that prevented an operation from completing
// but don't crash the application.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
//
// Example:
//
//	lighthouse.Error("Failed to process request: {error}", err.Error())
func Error(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelError, dt: time.Now(), message: msg, params: params}

	select {
	case logger.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		logger.write(LogLevelError, time.Now(), msg, params)
	}
}

// Error logs a message at the Error level on this logger instance.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
func (l *Logger) Error(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelError, dt: time.Now(), message: msg, params: params}

	select {
	case l.buffer <- logObj:
		// Successfully queued
	default:
		// Buffer full - write directly to avoid blocking
		l.write(LogLevelError, time.Now(), msg, params)
	}
}

// Panic logs a message at the Panic level and triggers a panic.
// The message is queued to the buffer (non-blocking). If the buffer is full,
// the message is written synchronously before panicking.
// The panic originates from processBuffer after the log is written.
//
// WARNING: This will crash the application after logging.
//
// Example:
//
//	lighthouse.Panic("Critical invariant violated: {detail}", "null pointer")
func Panic(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelPanic, dt: time.Now(), message: msg, params: params}

	select {
	case logger.buffer <- logObj:
		// Successfully queued, processBuffer will exit
	default:
		// Buffer full - write directly and exit
		logger.write(LogLevelPanic, time.Now(), msg, params)
		panic(msg)
	}
}

// Panic logs a message at the Panic level on this logger instance and triggers a panic.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
//
// WARNING: This will crash the application after logging.
func (l *Logger) Panic(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelPanic, dt: time.Now(), message: msg, params: params}

	select {
	case l.buffer <- logObj:
		// Successfully queued, processBuffer will exit
	default:
		// Buffer full - write directly and exit
		l.write(LogLevelPanic, time.Now(), msg, params)
		panic(msg)
	}
}

// Fatal logs a message at the Fatal level and terminates the application.
// The message is queued to the buffer (non-blocking). If the buffer is full,
// the message is written synchronously before exiting with code 1.
// The exit occurs from processBuffer after the log is written.
//
// WARNING: This will terminate the application immediately after logging.
//
// Example:
//
//	lighthouse.Fatal("Cannot start without configuration: {reason}", "missing API key")
func Fatal(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelFatal, dt: time.Now(), message: msg, params: params}

	select {
	case logger.buffer <- logObj:
		// Successfully queued, processBuffer will exit
	default:
		// Buffer full - write directly and exit
		logger.write(LogLevelFatal, time.Now(), msg, params)
		os.Exit(1)
	}
}

// Fatal logs a message at the Fatal level on this logger instance and terminates the application.
// Uses non-blocking buffer send with fallback to synchronous write if buffer is full.
//
// WARNING: This will terminate the application immediately after logging.
func (l *Logger) Fatal(message string, args ...interface{}) {
	msg, params := parseMessage(message, args...)
	logObj := logObject{level: LogLevelFatal, dt: time.Now(), message: msg, params: params}

	select {
	case l.buffer <- logObj:
		// Successfully queued, processBuffer will exit
	default:
		// Buffer full - write directly and exit
		l.write(LogLevelFatal, time.Now(), msg, params)
		os.Exit(1)
	}
}

// Debugf logs a formatted message at the Debug level using fmt.Sprintf formatting.
// Unlike Debug which uses template placeholders, Debugf uses standard printf formatting.
//
// Example:
//
//	lighthouse.Debugf("Processing %d items with %.2f%% success rate", count, rate)
func Debugf(message string, args ...interface{}) {
	Debug(fmt.Sprintf(message, args...))
}

// Debugf logs a formatted message at the Debug level on this logger instance using fmt.Sprintf formatting.
func (l *Logger) Debugf(message string, args ...interface{}) {
	l.Debug(fmt.Sprintf(message, args...))
}

// Infof logs a formatted message at the Info level using fmt.Sprintf formatting.
//
// Example:
//
//	lighthouse.Infof("Server listening on %s:%d", host, port)
func Infof(message string, args ...interface{}) {
	Info(fmt.Sprintf(message, args...))
}

// Infof logs a formatted message at the Info level on this logger instance using fmt.Sprintf formatting.
func (l *Logger) Infof(message string, args ...interface{}) {
	l.Info(fmt.Sprintf(message, args...))
}

// Warnf logs a formatted message at the Warn level using fmt.Sprintf formatting.
//
// Example:
//
//	lighthouse.Warnf("Response time %.2fms exceeds threshold %dms", actual, threshold)
func Warnf(message string, args ...interface{}) {
	Warn(fmt.Sprintf(message, args...))
}

// Warnf logs a formatted message at the Warn level on this logger instance using fmt.Sprintf formatting.
func (l *Logger) Warnf(message string, args ...interface{}) {
	l.Warn(fmt.Sprintf(message, args...))
}

// Errorf logs a formatted message at the Error level using fmt.Sprintf formatting.
//
// Example:
//
//	lighthouse.Errorf("Request failed: %v", err)
func Errorf(message string, args ...interface{}) {
	Error(fmt.Sprintf(message, args...))
}

// Errorf logs a formatted message at the Error level on this logger instance using fmt.Sprintf formatting.
func (l *Logger) Errorf(message string, args ...interface{}) {
	l.Error(fmt.Sprintf(message, args...))
}

// Panicf logs a formatted message at the Panic level using fmt.Sprintf formatting,
// then triggers a panic.
//
// WARNING: This will crash the application after logging.
//
// Example:
//
//	lighthouse.Panicf("Unexpected state: %v", invalidValue)
func Panicf(message string, args ...interface{}) {
	Panic(fmt.Sprintf(message, args...))
}

// Panicf logs a formatted message at the Panic level on this logger instance using fmt.Sprintf formatting,
// then triggers a panic.
//
// WARNING: This will crash the application after logging.
func (l *Logger) Panicf(message string, args ...interface{}) {
	l.Panic(fmt.Sprintf(message, args...))
}

// Fatalf logs a formatted message at the Fatal level using fmt.Sprintf formatting,
// then terminates the application with exit code 1.
//
// WARNING: This will terminate the application immediately after logging.
//
// Example:
//
//	lighthouse.Fatalf("Failed to initialize: %v", err)
func Fatalf(message string, args ...interface{}) {
	Fatal(fmt.Sprintf(message, args...))
}

// Fatalf logs a formatted message at the Fatal level on this logger instance using fmt.Sprintf formatting,
// then terminates the application with exit code 1.
//
// WARNING: This will terminate the application immediately after logging.
func (l *Logger) Fatalf(message string, args ...interface{}) {
	l.Fatal(fmt.Sprintf(message, args...))
}

// flush closes the log buffer and waits for all pending logs to be processed.
// This ensures no logs are lost during graceful shutdown.
// It is called automatically when SIGINT or SIGTERM is received.
//
// After calling flush, no new logs can be written.
func flush() {
	close(logger.buffer) // Signal processBuffer to exit after processing remaining items
	logger.wg.Wait()     // Wait for processBuffer to finish
}

// getTableCreation returns the SQL queries for creating the log table and index,
// plus the INSERT query template for the specified database type.
//
// Returns:
//   - createTableQuery: SQL to create the logs table
//   - createIndexQuery: SQL to create the timestamp index
//   - insertQuery: SQL template for inserting log entries
//   - error: nil or error if database type is unsupported
func getTableCreation(dbType LogDB) (string, string, string, error) {
	switch dbType {
	case LogDBSQLite:
		return sqliteCreateTableQuery, sqliteCreateIndexQuery, sqliteInsertQuery, nil
	case LogDBPostgreSQL:
		return postgreSQLCreateTableQuery, postgreSQLCreateIndexQuery, postgreSQLInsertQuery, nil
	case LogDBMySQL:
		return mysqlCreateTableQuery, mysqlCreateIndexQuery, mysqlInsertQuery, nil
	case LogDBMSSQL:
		return mssqlCreateTableQuery, mssqlCreateIndexQuery, mssqlInsertQuery, nil
	case LogDBOracle:
		return oracleCreateTableQuery, oracleCreateIndexQuery, oracleInsertQuery, nil
	default:
		return "", "", "", fmt.Errorf("unsupported database type")
	}
}

// sanitizeTableName removes dangerous characters from a table name to prevent SQL injection.
// It removes quotes and any non-alphanumeric characters (except underscores).
//
// Example:
//
//	safe := lighthouse.sanitizeTableName("logs; DROP TABLE users;--")
//	// safe = "logsDROPTABLEusers"
func sanitizeTableName(name string) string {
	name = strings.NewReplacer(`"`, "", `'`, "").Replace(name)
	reg := regexp.MustCompile(`[^a-zA-Z0-9_]+`)
	return reg.ReplaceAllString(name, "")
}
