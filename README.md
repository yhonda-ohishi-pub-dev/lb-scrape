# lb-scrape

スクレイパー負荷分散サービス。複数VPSへのスクレイピングリクエストを実行中ジョブ数ベースで動的に振り分ける。

## 概要

- **目的**: Cloud Run上で動作し、複数のVPSスクレイパーへリクエストを負荷分散
- **負荷分散ロジック**: 実行中ジョブ数が最も少ないhealthyなVPSを選択
- **ヘルスチェック**: 30秒キャッシュ付きオンデマンドチェック

## システム構成

```
Client
   │
   ▼
Cloud Run: scraper-api (ジョブ受付)
   │
   ▼
Cloud Run: scraper-lb  ← このサービス
   │
   ├──► VPS 1 (Scraper)
   └──► VPS 2 (Scraper)
   │
   ▼
Cloud SQL (PostgreSQL)
```

## API エンドポイント

| エンドポイント | メソッド | 説明 |
|---------------|---------|------|
| `/scrape` | POST | スクレイプ実行（負荷分散） |
| `/health` | GET | LBヘルスチェック |
| `/targets/status` | GET | VPS状態一覧（監視用） |

### POST /scrape

```json
{
  "job_id": 123,
  "job_type": "etc_meisai",
  "payload": { ... }
}
```

## 環境変数

| 変数名 | デフォルト | 説明 |
|--------|-----------|------|
| `PORT` | 8080 | サーバーポート |
| `DB_HOST` | localhost | PostgreSQLホスト |
| `DB_PORT` | 5432 | PostgreSQLポート |
| `DB_USER` | postgres | DBユーザー |
| `DB_PASSWORD` | - | DBパスワード |
| `DB_NAME` | myapp | DB名 |
| `DB_SSLMODE` | disable | SSL設定 |
| `CLOUDSQL_ENABLED` | false | Cloud SQL IAM認証を使用 |
| `CLOUDSQL_INSTANCE` | cloudsql-sv:asia-northeast1:postgres-prod | Cloud SQLインスタンス |
| `HEALTH_CHECK_CACHE_TTL` | 30 | ヘルスチェックキャッシュ秒数 |
| `VPS_BEARER_TOKEN` | - | VPS認証トークン |
| `VPS_REQUEST_TIMEOUT` | 55 | VPSリクエストタイムアウト秒数 |
| `USE_PARAM_MANAGER` | false | Parameter Manager使用 |
| `PARAM_NAME` | lb-scrape-config | パラメータ名 |
| `GCP_PROJECT` | cloudsql-sv | GCPプロジェクトID |

## Parameter Manager

`USE_PARAM_MANAGER=true` でParameter Managerから設定を取得。

### YAML形式

```yaml
port: "8080"
db_user: "user@example.com"
db_name: "myapp"
cloudsql_enabled: true
cloudsql_instance: "cloudsql-sv:asia-northeast1:postgres-prod"
health_check_cache_ttl: 30
vps_bearer_token: "secret-token"
vps_request_timeout: 55
```

Secret Managerへの参照も可能（Parameter Managerの機能）。

## データベース

PostgreSQLを使用。テーブル:
- `scraper_targets`: VPS管理
- `scraper_jobs`: ジョブ管理

スキーマは [schema.sql](schema.sql) 参照。

## 開発

```bash
# ビルド
go build -o lb-scrape .

# 実行
./lb-scrape

# Docker
docker build -t lb-scrape .
docker run -p 8080:8080 lb-scrape
```

## デプロイ

Cloud Runへのデプロイ:

```bash
gcloud run deploy scraper-lb \
  --source . \
  --region asia-northeast1 \
  --min-instances 0 \
  --max-instances 2 \
  --set-env-vars "CLOUDSQL_ENABLED=true,DB_USER=<IAM_USER>,DB_NAME=myapp"
```

**注意**: Cloud SQL IAM認証を使用する場合、サービスアカウントに `Cloud SQL Client` ロールが必要。

## 詳細設計

[scraper-system-design.md](scraper-system-design.md) 参照。
