/*
Copyright (c) YugabyteDB, Inc.

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
package tgtdb

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
)

type TargetDB interface {
	Init() error
	Finalize()
	InitConnPool() error
	CleanFileImportState(filePath, tableName string) error
	GetVersion() string
	CreateVoyagerSchema() error
	GetNonEmptyTables(tableNames []string) []string
	IsNonRetryableCopyError(err error) bool
	ImportBatch(batch Batch, args *ImportBatchArgs, exportDir string) (int64, error)
	IfRequiredQuoteColumnNames(tableName string, columns []string) ([]string, error)
	ExecuteBatch(migrationUUID uuid.UUID, batch *EventBatch) error
	GetDebeziumValueConverterSuite() map[string]ConverterFn
	GetEventChannelsMetaInfo(migrationUUID uuid.UUID) (map[int]EventChannelMetaInfo, error)
	GetTotalNumOfEventsImportedByType(migrationUUID uuid.UUID) (int64, int64, int64, error)
	InitLiveMigrationState(migrationUUID uuid.UUID, numChans int, startClean bool, tableNames []string) error
	MaxBatchSizeInBytes() int64
	RestoreSequences(sequencesLastValue map[string]int64) error
}

const (
	ORACLE     = "oracle"
	MYSQL      = "mysql"
	POSTGRESQL = "postgresql"
	YUGABYTEDB = "yugabytedb"
)

// value converter Function type
type ConverterFn func(v string, formatIfRequired bool) (string, error)

type Batch interface {
	Open() (*os.File, error)
	GetFilePath() string
	GetTableName() string
	GetQueryIsBatchAlreadyImported() string
	GetQueryToRecordEntryInDB(rowsAffected int64) string
}

func NewTargetDB(tconf *TargetConf) TargetDB {
	if tconf.TargetDBType == "oracle" {
		return newTargetOracleDB(tconf)
	}
	return newTargetYugabyteDB(tconf)
}

type ImportBatchArgs struct {
	FilePath  string
	TableName string
	Columns   []string

	FileFormat string
	HasHeader  bool
	Delimiter  string
	QuoteChar  byte
	EscapeChar byte
	NullString string

	RowsPerTransaction int64
}

func (args *ImportBatchArgs) GetYBCopyStatement() string {
	columns := ""
	if len(args.Columns) > 0 {
		columns = fmt.Sprintf("(%s)", strings.Join(args.Columns, ", "))
	}
	options := []string{
		fmt.Sprintf("FORMAT '%s'", args.FileFormat),
		fmt.Sprintf("ROWS_PER_TRANSACTION %v", args.RowsPerTransaction),
	}
	if args.HasHeader {
		options = append(options, "HEADER")
	}
	if args.Delimiter != "" {
		options = append(options, fmt.Sprintf("DELIMITER E'%c'", []rune(args.Delimiter)[0]))
	}
	if args.QuoteChar != 0 {
		quoteChar := string(args.QuoteChar)
		if quoteChar == `'` || quoteChar == `\` {
			quoteChar = `\` + quoteChar
		}
		options = append(options, fmt.Sprintf("QUOTE E'%s'", quoteChar))
	}
	if args.EscapeChar != 0 {
		escapeChar := string(args.EscapeChar)
		if escapeChar == `'` || escapeChar == `\` {
			escapeChar = `\` + escapeChar
		}
		options = append(options, fmt.Sprintf("ESCAPE E'%s'", escapeChar))
	}
	if args.NullString != "" {
		options = append(options, fmt.Sprintf("NULL '%s'", args.NullString))
	}
	return fmt.Sprintf(`COPY %s %s FROM STDIN WITH (%s)`, args.TableName, columns, strings.Join(options, ", "))
}

func (args *ImportBatchArgs) GetSqlLdrControlFile(schema string) string {
	var columns string
	if len(args.Columns) > 0 {
		columnsSlice := make([]string, 0, len(args.Columns))
		for _, col := range args.Columns {
			// Add the column name and the NULLIF clause after it
			columnsSlice = append(columnsSlice, fmt.Sprintf(`%s NULLIF %s='\\N'`, col, col))
		}
		columns = fmt.Sprintf("(%s)", strings.Join(columnsSlice, ", "))
	}

	configTemplate := `LOAD DATA
INFILE '%s'
APPEND
INTO TABLE %s
REENABLE DISABLED_CONSTRAINTS
FIELDS TERMINATED BY '%s'
%s`
	return fmt.Sprintf(configTemplate, args.FilePath, schema+"."+args.TableName, "\\t", columns)
}
