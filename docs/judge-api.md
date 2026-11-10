# Judge API

評測 worker 透過 HTTP 向後端領取工作並回報結果。所有請求都要帶
`Authorization: Bearer <JUDGE_TOKEN>`；後端未設定 `JUDGE_TOKEN` 時這些端點回應 404。
這組端點不該對外公開，建議只在內部網路開放。

## `POST /api/judge/claim`

領取佇列中優先度最高的一筆提交，並把它標記為 Judging（state 100）。
沒有工作時回應 `204 No Content`，worker 應稍候再試。

```json
{
  "id": 42,
  "language": "g++",
  "code": "...",
  "problem": {
    "id": 7,
    "time_limit_ms": 1000,
    "memory_limit_kb": 262144,
    "checker": "diff",
    "testcases": [{ "id": 15, "name": "1", "input_size": 4, "output_size": 2 }],
    "subtasks": [{ "idx": 1, "score": 100, "testcase_ids": [15], "depends": [] }]
  }
}
```

領取後 10 分鐘內沒有回報，該提交會自動回到佇列。

## `GET /api/judge/testcases/{id}/in` 與 `/out`

下載測資的輸入與標準輸出。

## `POST /api/judge/submissions/{id}/result`

無法執行時（編譯錯誤等）回報整體狀態：

```json
{ "state": 9, "message": "compiler output" }
```

可用的 `state`：9 CE、10 CLE、11 IE、12 JE。

有執行測資時回報每筆測資的結果，`state` 留 0 或省略：

```json
{
  "testcases": [
    { "id": 15, "state": 1, "time_ms": 12, "memory_kb": 1024, "message": "", "ratio": 0 }
  ]
}
```

測資 `state`：1 AC、2 PC（搭配 `ratio` 0–1 表示得分比例）、3 WA、4 RE、5 RE(SIG)、6 TLE、7 MLE、
8 OLE、11 IE、12 JE、102 Skipped。未回報的測資視為 Skipped。

子任務與總分由後端依題目設定計算。提交不在 Judging 狀態時（例如已被重測）回應 `409`，worker 應丟棄結果。
