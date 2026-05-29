package surrealdb

import (
	"fmt"
	"strings"

	sdkModels "github.com/surrealdb/surrealdb.go/pkg/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/dailaim/surrealdb-gorm/clauses"
	TypesM "github.com/dailaim/surrealdb-gorm/types"
)

func QueryCallback(db *gorm.DB) {
	if db.Error != nil {
		return
	}

	// Manual Soft Delete check
	if db.Statement.Schema != nil && db.Statement.Schema.LookUpField("DeletedAt") != nil && !db.Statement.Unscoped {
		db.Statement.AddClause(clause.Where{
			Exprs: []clause.Expression{
				clause.Expr{SQL: "`deleted_at` IS NULL OR `deleted_at` IS NONE"},
			},
		})
	}

	// Ensure default clauses for SELECT if missing
	if len(db.Statement.BuildClauses) == 0 {
		db.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "GROUP BY", "ORDER BY", "LIMIT", "FOR", "INFO", "FETCH"}
	}
	if _, ok := db.Statement.Clauses["SELECT"]; !ok {
		selectSQL := "*"
		if gs, ok := db.Statement.Clauses["GRAPH_SELECT"]; ok {
			if gsExpr, ok := gs.Expression.(clauses.GraphSelect); ok {
				for _, f := range gsExpr.Fields {
					selectSQL += ", " + f
				}
			}
		}
		db.Statement.AddClause(clause.Select{Expression: clause.Expr{SQL: selectSQL}})
	} else if gs, ok := db.Statement.Clauses["GRAPH_SELECT"]; ok {
		if gsExpr, ok := gs.Expression.(clauses.GraphSelect); ok && len(gsExpr.Fields) > 0 {
			extra := strings.Join(gsExpr.Fields, ", ")
			selClause := db.Statement.Clauses["SELECT"]
			if expr, ok := selClause.Expression.(clause.Select); ok {
				if sqlExpr, ok := expr.Expression.(clause.Expr); ok {
					expr.Expression = clause.Expr{SQL: sqlExpr.SQL + ", " + extra}
				} else {
					expr.Expression = clause.Expr{SQL: "*, " + extra}
				}
				selClause.Expression = expr
				db.Statement.Clauses["SELECT"] = selClause
			}
		}
	}
	if _, ok := db.Statement.Clauses["FROM"]; !ok && db.Statement.Table != "" {
		db.Statement.AddClause(clause.From{Tables: []clause.Table{{Name: db.Statement.Table}}})
	}

	db.Statement.Build(db.Statement.BuildClauses...)
	if db.Error != nil {
		return
	}

	optimizeFindByID(db)
	executeSQL(db)
}

func optimizeFindByID(db *gorm.DB) {
	if len(db.Statement.Vars) >= 1 {
		isRecordID := false
		switch db.Statement.Vars[0].(type) {
		case *sdkModels.RecordID, sdkModels.RecordID,
			*TypesM.RecordID, TypesM.RecordID:
			isRecordID = true
		}
		if isRecordID {
			sql := db.Statement.SQL.String()
			if db.Statement.Table != "" {
				quotedTable := fmt.Sprintf("`%s`", db.Statement.Table)
				targetFrom := fmt.Sprintf("FROM %s", quotedTable)
				newFrom := "FROM $p1"

				if strings.Contains(sql, targetFrom) && (strings.Contains(sql, "`id` = $p1") || strings.Contains(sql, "id = $p1")) {
					sql = strings.Replace(sql, targetFrom, newFrom, 1)

					sql = strings.ReplaceAll(sql, fmt.Sprintf("WHERE %s.`id` = $p1 AND ", quotedTable), "WHERE ")
					sql = strings.ReplaceAll(sql, "WHERE `id` = $p1 AND ", "WHERE ")
					sql = strings.ReplaceAll(sql, "WHERE id = $p1 AND ", "WHERE ")

					sql = strings.ReplaceAll(sql, fmt.Sprintf(" AND %s.`id` = $p1", quotedTable), "")
					sql = strings.ReplaceAll(sql, " AND `id` = $p1", "")
					sql = strings.ReplaceAll(sql, " AND id = $p1", "")

					sql = strings.ReplaceAll(sql, fmt.Sprintf("WHERE %s.`id` = $p1", quotedTable), "")
					sql = strings.ReplaceAll(sql, "WHERE `id` = $p1", "")
					sql = strings.ReplaceAll(sql, "WHERE id = $p1", "")
					sql = strings.ReplaceAll(sql, "AND id = $p1 AND ", "AND")

					sql = strings.ReplaceAll(sql, fmt.Sprintf("ORDER BY %s.`id`", quotedTable), "")
					sql = strings.ReplaceAll(sql, "ORDER BY `id`", "")

					sql = strings.ReplaceAll(sql, "  ", " ")

					db.Statement.SQL.Reset()
					db.Statement.SQL.WriteString(sql)
				}
			}
		}
	}
}
