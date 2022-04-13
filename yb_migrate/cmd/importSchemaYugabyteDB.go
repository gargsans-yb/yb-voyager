/*
Copyright (c) YugaByte, Inc.

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
package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yugabyte/ybm/yb_migrate/src/migration"
	"github.com/yugabyte/ybm/yb_migrate/src/utils"

	"github.com/jackc/pgx/v4"
)

func PrintTargetYugabyteDBVersion(target *utils.Target) {
	targetConnectionURI := target.GetConnectionUri()

	version := migration.SelectVersionQuery("yugabytedb", targetConnectionURI)
	fmt.Printf("YugabyteDB Version: %s\n", version)
}

func YugabyteDBImportSchema(target *utils.Target, exportDir string) {
	metaInfo := ExtractMetaInfo(exportDir)

	projectDirPath := exportDir

	targetConnectionURI := ""
	if target.Uri == "" {
		targetConnectionURI = fmt.Sprintf("postgresql://%s:%s@%s:%d/%s?%s",
			target.User, target.Password, target.Host, target.Port, target.DBName, generateSSLQueryStringIfNotExists(target))
	} else {
		targetConnectionURI = target.Uri
	}

	//this list also has defined the order to create object type in target YugabyteDB
	importObjectOrderList := utils.GetSchemaObjectList(metaInfo.SourceDBType)

	for _, importObjectType := range importObjectOrderList {
		var importObjectDirPath, importObjectFilePath string

		if importObjectType != "INDEX" {
			importObjectDirPath = projectDirPath + "/schema/" + strings.ToLower(importObjectType) + "s"
			importObjectFilePath = importObjectDirPath + "/" + strings.ToLower(importObjectType) + ".sql"
		} else {
			if target.ImportIndexesAfterData {
				continue
			}
			importObjectDirPath = projectDirPath + "/schema/" + "tables"
			importObjectFilePath = importObjectDirPath + "/" + "INDEXES_table.sql"
		}

		if !utils.FileOrFolderExists(importObjectFilePath) {
			continue
		}

		fmt.Printf("importing %10s %5s", importObjectType, "")
		go utils.Wait("done\n", "")

		conn, err := pgx.Connect(context.Background(), targetConnectionURI)
		if err != nil {
			utils.WaitChannel <- 1
			<-utils.WaitChannel
			fmt.Println(err)
			os.Exit(1)
		}

		// target-db-schema is not public and source is either Oracle/MySQL
		if metaInfo.SourceDBType != POSTGRESQL {
			setSchemaQuery := fmt.Sprintf("SET SCHEMA '%s'", target.Schema)
			_, err := conn.Exec(context.Background(), setSchemaQuery)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		}

		sqlStrArray := createSqlStrArray(importObjectFilePath, importObjectType)
		errOccured := 0
		for _, sqlStr := range sqlStrArray {
			// fmt.Printf("Execute STATEMENT: %s\n", sqlStr[1])
			_, err := conn.Exec(context.Background(), sqlStr[0])
			if err != nil {
				if strings.Contains(err.Error(), "already exists") {
					if !target.IgnoreIfExists {
						fmt.Printf("\b \n    %s\n", err.Error())
						fmt.Printf("    STATEMENT: %s\n", sqlStr[1])
						if !target.ContinueOnError {
							os.Exit(1)
						}
					}
				} else {
					errOccured = 1
					fmt.Printf("\b \n    %s\n", err.Error())
					fmt.Printf("    STATEMENT: %s\n", sqlStr[1])
					if !target.ContinueOnError { //default case
						fmt.Println(err)
						os.Exit(1)
					}
				}
			}
		}

		utils.WaitChannel <- errOccured
		<-utils.WaitChannel

		conn.Close(context.Background())
	}

}

//This function is implementation is rough as of now.
func ExtractMetaInfo(exportDir string) utils.ExportMetaInfo {
	// fmt.Printf("Extracting the metainfo about the source database...\n")
	var metaInfo utils.ExportMetaInfo

	metaInfoDirPath := exportDir + "/metainfo"

	metaInfoDir, err := os.ReadDir(metaInfoDirPath)
	utils.CheckError(err, "", "", true)

	for _, metaInfoSubDir := range metaInfoDir {
		// fmt.Printf("%s\n", metaInfoSubDir.Name())

		if metaInfoSubDir.IsDir() {
			subItemPath := metaInfoDirPath + "/" + metaInfoSubDir.Name()

			subItems, err := os.ReadDir(subItemPath)
			if err != nil {
				panic(err)
			}
			for _, subItem := range subItems {
				subItemName := subItem.Name()
				// fmt.Printf("\t%s\n", subItemName)

				if strings.HasPrefix(subItemName, "source-db-") {
					splits := strings.Split(subItemName, "-")

					metaInfo.SourceDBType = splits[len(splits)-1]
				}

			}
		}

	}

	// fmt.Printf("MetaInfo Struct: %v\n", metaInfo)
	return metaInfo
}