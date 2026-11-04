package main

import (
  "fmt"
  "os"
  "gorm.io/driver/sqlite"
  "gorm.io/gorm"
)

type Dict struct {
  ID         uint64  `gorm:"column:id"`
  GroupCode  string  `gorm:"column:group_code"`
  ItemKey    string  `gorm:"column:item_key"`
  ItemValue  string  `gorm:"column:item_value"`
  Label      string  `gorm:"column:label"`
  Sort       int     `gorm:"column:sort"`
  IsActive   int     `gorm:"column:is_active"`
  Description string `gorm:"column:description"`
}

func main() {
  db, err := gorm.Open(sqlite.Open("./data/dev.db"), &gorm.Config{})
  if err != nil { fmt.Println("err:", err); os.Exit(1) }
  var rows []Dict
  db.Raw("SELECT id, group_code, item_key, item_value, label, sort, is_active, description FROM sys_dict ORDER BY group_code, sort, id").Scan(&rows)
  active := 0
  for _, r := range rows { if r.IsActive == 1 { active++ } }
  fmt.Printf("Total: %d, Active: %d, Disabled: %d\n\n", len(rows), active, len(rows)-active)
  fmt.Println("=== Disabled (is_active=0) 全量 ===")
  for _, r := range rows {
    if r.IsActive == 0 {
      fmt.Printf("  %-25s %-30s %-15s sort=%d\n", r.GroupCode, r.ItemKey, r.ItemValue, r.Sort)
    }
  }
}
