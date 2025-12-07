# CLAUDE.md

lb-scrapeプロジェクトの開発ガイド。

## プロジェクト概要

スクレイパー負荷分散サービス（Go 1.21）。Cloud Run上で動作し、複数VPSへのスクレイピングリクエストを動的に振り分ける。

## ディレクトリ構成

```
lb-scrape/
├── main.go              # エントリポイント、HTTP+gRPC+grpc-webサーバー
├── config/
│   ├── config.go        # 環境変数設定
│   └── params.go        # Parameter Manager連携
├── db/db.go             # PostgreSQL接続（Cloud SQL IAM認証対応）
├── models/models.go     # データモデル定義
├── service/
│   ├── loadbalancer.go  # 負荷分散ロジック、ジョブ管理
│   └── healthcheck.go   # VPSヘルスチェック（30秒キャッシュ）
├── handler/handler.go   # HTTPハンドラー（VPS IAM認証対応）
├── proto/scraper.proto  # gRPCサービス定義
├── pkg/
│   ├── pb/              # protoc生成コード
│   └── grpc/            # gRPC/grpc-webサーバー実装
├── cmd/crudtest/        # 本番DB接続CRUDテストツール
├── schema.sql           # DBスキーマ（開発用）
└── Dockerfile           # Cloud Run用
```

## 主要コンポーネント

### LoadBalancer (`service/loadbalancer.go`)
- `SelectTarget()`: 実行中ジョブ数最小のhealthyなVPSを選択
- `UpdateJobStatus()`: ジョブステータス更新
- `UpdateJobResult()`: 結果/エラー記録

### HealthChecker (`service/healthcheck.go`)
- 30秒TTLキャッシュ付きヘルスチェック
- VPSの `/health` エンドポイントへGET

### Handler (`handler/handler.go`)
- `POST /scrape`: VPS選択→ヘルスチェック→リクエスト転送→結果更新
- `GET /health`: LB自身のヘルスチェック
- `GET /targets/status`: 全VPSの状態一覧

### gRPC/grpc-web (`pkg/grpc/`)
- `ScraperService`: HTTP APIと同等のgRPCサービス
- grpc-web対応でブラウザから直接呼び出し可能
- h2cでHTTP/2 cleartext対応（Cloud Run用）

## 開発コマンド

```bash
# ビルド
go build -o lb-scrape .

# テスト
go test ./...

# 依存整理
go mod tidy

# proto再生成
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       proto/scraper.proto
mv proto/*.pb.go pkg/pb/
```

## データベース

PostgreSQL使用。テーブル:
- `scraper_targets`: VPS管理（id, name, url, healthy, last_checked）
- `scraper_jobs`: ジョブ管理（status: pending→running→completed/failed）

### 本番マイグレーション

本番DBは `cloudsql` プロジェクトで管理:
- `000013_scraper_targets.up.sql` - targetsテーブル
- `000014_scraper_jobs.up.sql` - jobsテーブル（organization_id + RLS対応）
- `000015_scraper_grants.up.sql` - 権限付与

**注意**: 本番の `scraper_jobs` は `organization_id` カラムを持ちマルチテナント対応。
ローカルの `schema.sql` は開発用簡易版。

## 設定管理

### Parameter Manager
`USE_PARAM_MANAGER=true` でGCP Parameter ManagerからYAML設定を取得。
- `PARAM_VERSION`: パラメータバージョン指定（デフォルト: "latest"、v1, v2等も可）
- 優先順位: Parameter Manager > 環境変数 > デフォルト

### cmd/crudtest
本番DB接続のCRUDテストツール:
```bash
# Cloud SQL Proxy経由で実行
go run ./cmd/crudtest
```
- scraper_targets: CRUD操作
- scraper_jobs: RLSセッション設定込みのCRUD

## gRPCテスト

```bash
# ヘルスチェック
grpcurl -plaintext localhost:8080 scraper.ScraperService/Health

# ターゲット状態
grpcurl -plaintext localhost:8080 scraper.ScraperService/TargetsStatus
```

## 環境変数

| 変数名 | 説明 | デフォルト |
|--------|------|-----------|
| `PORT` | HTTP/gRPCポート | 8080 |
| `GRPC_PORT` | (未使用) gRPC専用ポート | 50051 |
| `GRPC_WEB_ENABLED` | grpc-web有効化 | true |
| `ALLOWED_ORIGINS` | CORS許可オリジン（カンマ区切り） | * |

## 注意点

- VPSへのリクエストはBearer Token認証またはIAM認証（ID Token）
- ヘルスチェックはオンデマンド（リクエスト時のみ）
- ジョブステータスはDB側で管理（scraper-apiが作成、lb-scrapeが更新）
- Cloud SQL IAM認証使用時はサービスアカウントに `Cloud SQL Client` ロール必要
- 単一ポートでHTTP REST / gRPC / grpc-webを提供（h2c使用）
