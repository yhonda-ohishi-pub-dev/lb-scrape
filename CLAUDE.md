# CLAUDE.md

lb-scrapeプロジェクトの開発ガイド。

## プロジェクト概要

スクレイパー負荷分散サービス（Go 1.21）。Cloud Run上で動作し、複数VPSへのスクレイピングリクエストを動的に振り分ける。

## ディレクトリ構成

```
lb-scrape/
├── main.go              # エントリポイント、ルーティング設定
├── config/config.go     # 環境変数設定
├── db/db.go             # PostgreSQL接続
├── models/models.go     # データモデル定義
├── service/
│   ├── loadbalancer.go  # 負荷分散ロジック、ジョブ管理
│   └── healthcheck.go   # VPSヘルスチェック（30秒キャッシュ）
├── handler/handler.go   # HTTPハンドラー
├── schema.sql           # DBスキーマ
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

## 開発コマンド

```bash
# ビルド
go build -o lb-scrape .

# テスト
go test ./...

# 依存整理
go mod tidy
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

## 注意点

- VPSへのリクエストはBearer Token認証
- ヘルスチェックはオンデマンド（リクエスト時のみ）
- ジョブステータスはDB側で管理（scraper-apiが作成、lb-scrapeが更新）
