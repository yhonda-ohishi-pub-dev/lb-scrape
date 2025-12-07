# スクレイパーシステム設計書

## 1. 概要

GCP上でスクレイパーを運用するためのシステム設計。Cloud Runでリクエスト管理・負荷分散を行い、複数VPSでスクレイピングを実行する。

### 1.1 設計方針

- 責務分離: LBとAPIを別サービスに分離
- 負荷分散: 実行中ジョブ数ベースの動的振り分け
- コスト最適化: Cloud Run min-instances=0 でアイドル時課金なし
- 状態管理: 既存Cloud SQLを活用

---

## 2. システム構成

```
┌─────────────────────────────────────────────────────────────┐
│                        Client                                │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  Cloud Run: scraper-api                      │
│                  (ジョブ受付・結果取得)                       │
│                  min-instances: 0                            │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  Cloud Run: scraper-lb                       │
│                  (負荷分散・ヘルスチェック)                   │
│                  min-instances: 0                            │
└──────────────┬──────────────┴──────────────┬────────────────┘
               │                             │
               ▼                             ▼
┌──────────────────────────┐   ┌──────────────────────────┐
│      GCP VPS 1           │   │      GCP VPS 2           │
│      Scraper             │   │      Scraper             │
│      固定IP: A           │   │      固定IP: B           │
└──────────────────────────┘   └──────────────────────────┘
               │                             │
               └──────────────┬──────────────┘
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                       Cloud SQL                              │
│                       (MySQL)                                │
│                       ジョブ状態管理                          │
└─────────────────────────────────────────────────────────────┘
```

---

## 3. データベース設計

### 3.1 scraper_targets（VPS管理）

| カラム | 型 | 説明 |
|--------|-----|------|
| id | INT | PK, AUTO_INCREMENT |
| name | VARCHAR(50) | VPS識別名 (UNIQUE) |
| url | VARCHAR(255) | VPSのエンドポイントURL |
| healthy | BOOLEAN | ヘルス状態 (DEFAULT: TRUE) |
| last_checked | TIMESTAMP | 最終ヘルスチェック日時 |

```sql
CREATE TABLE scraper_targets (
    id           INT PRIMARY KEY AUTO_INCREMENT,
    name         VARCHAR(50) NOT NULL UNIQUE,
    url          VARCHAR(255) NOT NULL,
    healthy      BOOLEAN DEFAULT TRUE,
    last_checked TIMESTAMP NULL
);
```

### 3.2 scraper_jobs（ジョブ管理）

| カラム | 型 | 説明 |
|--------|-----|------|
| id | BIGINT | PK, AUTO_INCREMENT |
| job_type | VARCHAR(50) | ジョブ種別 (etc_meisai等) |
| payload | JSON | リクエストパラメータ |
| status | ENUM | pending/running/completed/failed |
| target_id | INT | 割り当てVPS (FK) |
| created_at | TIMESTAMP | 作成日時 |
| started_at | TIMESTAMP | 開始日時 |
| completed_at | TIMESTAMP | 完了日時 |
| result | JSON | 実行結果 |
| error_message | TEXT | エラーメッセージ |
| retry_count | INT | リトライ回数 |

```sql
CREATE TABLE scraper_jobs (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    job_type      VARCHAR(50) NOT NULL,
    payload       JSON,
    status        ENUM('pending', 'running', 'completed', 'failed') DEFAULT 'pending',
    target_id     INT,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at    TIMESTAMP NULL,
    completed_at  TIMESTAMP NULL,
    result        JSON NULL,
    error_message TEXT NULL,
    retry_count   INT DEFAULT 0,
    
    FOREIGN KEY (target_id) REFERENCES scraper_targets(id),
    INDEX idx_status (status),
    INDEX idx_target_status (target_id, status)
);
```

---

## 4. 負荷分散ロジック

### 4.1 アルゴリズム

実行中ジョブ数が最も少ないhealthyなVPSを選択する。

```sql
SELECT 
    t.id, 
    t.url, 
    t.name,
    COUNT(j.id) as running_count
FROM scraper_targets t
LEFT JOIN scraper_jobs j 
    ON t.id = j.target_id AND j.status = 'running'
WHERE t.healthy = TRUE
GROUP BY t.id
ORDER BY running_count ASC, t.id ASC
LIMIT 1;
```

### 4.2 ヘルスチェック

- 方式: オンデマンド（リクエスト時）
- キャッシュTTL: 30秒
- チェック方法: VPSの `/health` エンドポイントへGET

---

## 5. 処理フロー

### 5.1 ジョブ実行フロー

```
1. Client → scraper-api: ジョブリクエスト
2. scraper-api → Cloud SQL: ジョブ INSERT (status: pending)
3. scraper-api → scraper-lb: スクレイプ依頼
4. scraper-lb → Cloud SQL: 負荷分散クエリ実行
5. scraper-lb: ヘルスチェック (キャッシュ期限切れの場合)
6. scraper-lb → Cloud SQL: ジョブ UPDATE (status: running, target_id, started_at)
7. scraper-lb → VPS: HTTPリクエスト (Bearer Token認証)
8. VPS → scraper-lb: 結果返却
9. scraper-lb → scraper-api: 結果返却
10. scraper-api → Cloud SQL: ジョブ UPDATE (status: completed/failed)
11. scraper-api → Client: レスポンス
```

### 5.2 状態遷移図

```
┌─────────┐
│ pending │ ← ジョブ作成
└────┬────┘
     │ VPS割り当て & 開始
     ▼
┌─────────┐
│ running │
└────┬────┘
     │
     ├─────────────┐
     ▼             ▼
┌───────────┐ ┌────────┐
│ completed │ │ failed │
└───────────┘ └────┬───┘
                   │ リトライ (上限内)
                   ▼
              ┌─────────┐
              │ pending │
              └─────────┘
```

---

## 6. API仕様

### 6.1 scraper-api

| エンドポイント | メソッド | 説明 |
|---------------|---------|------|
| /jobs | POST | ジョブ作成 |
| /jobs/{id} | GET | ジョブ状態取得 |
| /jobs/{id}/result | GET | ジョブ結果取得 |

### 6.2 scraper-lb

| エンドポイント | メソッド | 説明 |
|---------------|---------|------|
| /scrape | POST | スクレイプ実行（内部用） |
| /health | GET | LBヘルスチェック |
| /targets/status | GET | VPS状態一覧（監視用） |

### 6.3 VPS (Scraper)

| エンドポイント | メソッド | 説明 |
|---------------|---------|------|
| /scrape | POST | スクレイプ実行 |
| /health | GET | ヘルスチェック |

---

## 7. セキュリティ

### 7.1 認証

- scraper-api ↔ scraper-lb: Cloud Run内部通信（IAM認証）
- scraper-lb ↔ VPS: Bearer Token認証

### 7.2 通信

- HTTPS必須
- VPSはCloud RunのIPのみ許可（ファイアウォール）

---

## 8. Cloud Run設定

### 8.1 scraper-api

```yaml
service: scraper-api
min-instances: 0
max-instances: 3
cpu: 1
memory: 512Mi
concurrency: 80
timeout: 300s
```

### 8.2 scraper-lb

```yaml
service: scraper-lb
min-instances: 0
max-instances: 2
cpu: 1
memory: 256Mi
concurrency: 100
timeout: 60s
```

---

## 9. 運用

### 9.1 タイムアウト処理

- 条件: running状態で10分以上経過
- 処理: Cloud Schedulerで定期チェック → failed に更新

### 9.2 リトライ

- 上限: 3回
- 対象: failed かつ retry_count < 3

### 9.3 データ保持

- 完了ジョブ: 30日で自動削除
- 失敗ジョブ: 90日で自動削除

---

## 10. 今後の拡張

- VPS自動スケール（負荷に応じてVPS追加/削除）
- ジョブ優先度（priority カラム追加）
- 重複リクエスト防止（idempotency key）
