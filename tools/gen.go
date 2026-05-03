package main

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gen"
	"gorm.io/gorm"
)

func init() {

}
func main() {
	// 从环境变量读取 DSN，如果没有则使用默认值
	// dsn := os.Getenv("DB_DSN")
	// if dsn == "" {
	dsn := "oss_db:fcBPX7kHrdWs83H2@tcp(101.34.54.199:23560)/oss_db?charset=utf8mb4&parseTime=True&loc=Local"
	// }

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(fmt.Sprintf("连接数据库失败: %v", err))
	}

	g := gen.NewGenerator(gen.Config{
		OutPath: "./internal/db/model",
		Mode:    gen.WithDefaultQuery,
	})

	g.UseDB(db)

	// 从数据库自动生成所有表的 model
	g.ApplyBasic(g.GenerateAllTable()...)

	// 生成代码
	g.Execute()
	fmt.Println("✅ GORM model 生成完成！")
}
