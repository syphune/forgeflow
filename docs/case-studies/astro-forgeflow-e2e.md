# ASTRO-1: Từ production bug đến merged PR bằng Forgeflow

Ngày thực hiện: 2026-08-26; cập nhật full-system E2E: 2026-08-28  
Dự án: `ASTRO` — Tinh đồ AI  
Work item: `ASTRO-1` — sửa mapping role và layout mobile của relationship reader  
Repository: [`syphune/astro-birth-ai`](https://github.com/syphune/astro-birth-ai)  
Commit triển khai: [`03a8ce311ac4c143c073c55fe1bcbdf83dc145ef`](https://github.com/syphune/astro-birth-ai/commit/03a8ce311ac4c143c073c55fe1bcbdf83dc145ef)  
Commit CI: `d4f96ebdc3de8e78a492467af1c4aed0a64a85dc`, `b5122f4`, `dc2fee9`, `f70d1f6`, `a5591ca`, `f7b4b9e`, `1b64074`  
PR: [#1 — Implement ASTRO-1](https://github.com/syphune/astro-birth-ai/pull/1)

## Kết quả

ASTRO-1 xử lý hai biểu hiện của cùng một lỗi production trong sản phẩm Astro:

1. Câu văn bình thường như `Anh cần nói rõ điều này` và `Chị muốn thêm thời gian` được giữ nguyên; chỉ các nhãn vai trò có cấu trúc như `Người A`, `Người B`, `Người 1 (A)`, `Người 2 (B)` mới được đổi thành `Bạn`, `Người ấy`.
2. Reader relationship trên mobile không còn mang margin/width desktop sang viewport 390–412px; evidence drawer và chat nằm gọn trong viewport.

Forgeflow đưa bug từ prompt của người dùng đến một merged PR thật: intake bằng agent, specification được người dùng xác minh (human verification), branch/AgentRun, interruption recovery, code review có request changes, CI gate, QA và merge. Đây là một workflow có cả failure và recovery, không chỉ là happy path.

ASTRO-1 chạy trên **Default workflow**: `DONE` ở đây nghĩa là development work đã hoàn tất, không phải bằng chứng đã deploy production. Case study này không tuyên bố autonomous production deployment; merge production/release và deployment vẫn là human gate.

Evidence release chính: 367 backend tests, 27 frontend tests, 50 E2E của sản phẩm Astro (cộng 2 snapshot được skip theo thiết kế), và CI run 23 với 23 step thành công. Cả 22 user-facing outcome được định nghĩa cho release ban đầu đều đạt. Bản kiểm tra Forgeflow bổ sung ngày 2026-08-28 thêm vòng đời Done giống Jira, nâng coverage user-facing lên 23 outcome; chi tiết nằm trong technical appendix.

Trạng thái cuối Forgeflow là `DONE`, version `10`. Lần chuyển sang `QA` đầu tiên bị chặn đúng bởi rule `successful CI is required before QA`; sau khi CI xanh trên đúng head, transition service cho phép `CODE_REVIEW v8 → QA v9 → DONE v10`. PR được merge thật với merge commit `fa3a5d2e3c7ddd59484cbf26451742d7151bc45a`.

### Before → After

| Trường hợp | Trước | Sau |
|---|---|---|
| Prose: `Anh cần nói rõ điều này` | Bị đổi nhầm thành `Người ấy cần nói rõ điều này` | Giữ nguyên câu gốc |
| Role `Người A` | Không có mapping role đủ chặt | Đổi thành `Bạn` |
| Role `Người B` | Không có mapping role đủ chặt | Đổi thành `Người ấy` |
| Reader mobile | Panel kế thừa layout desktop, gây overflow | Vừa viewport 390–412px, không overflow |

## 1. Báo bug như một người dùng thật

Trong project Astro, mở Backlog và chọn tạo bug bằng AI server. Nội dung đưa cho agent phải mô tả cả lỗi, phạm vi và tiêu chí nghiệm thu; không chỉ nói “sửa replace”. Prompt đã dùng ở lần intake:

```text
Trong dự án Astro, phần luận giải mối quan hệ đang có lỗi production: logic replace đang biến các đại từ thông thường như “anh”, “Anh ấy”, “chị”, “Chị ấy” trong prose thành “Người ấy”. Đây không phải participant role và làm sai nghĩa câu.

Hãy tạo một BUG có tiêu đề và đặc tả đủ để developer sửa an toàn:
- Chỉ mapping các role có cấu trúc rõ ràng: Người 1 (A) → Bạn, Người 2 (B) → Người ấy, Người A → Bạn, Người B → Người ấy, hoặc token A/B độc lập trong ngữ cảnh relationship.
- Không thay đổi “Anh cần nói rõ điều này”, “anh ấy đang suy nghĩ”, “Chị muốn thêm thời gian”, “chị ấy đã gọi lại”.
- Kiểm tra mọi caller của cleanReaderLanguage và relationshipReaderText để không làm hỏng generic/personal reader.
- Kiểm tra CSS của chat-panel-v2/evidence drawer ở viewport mobile 390–412px: không margin desktop, không horizontal overflow, panel nằm trong viewport.
- Acceptance: unit test backend report, frontend report-reading, E2E relationship desktop/mobile, kiểm tra prose giữ nguyên, role A/B vẫn map đúng, evidence drawer và chat phủ đúng viewport.
- Không sửa status bằng generic update; mọi status change phải qua transition service; không đưa secret hoặc chạy code tùy ý từ repository.
```

Agent đã tạo `ASTRO-1` dạng `BUG`. Ảnh thao tác:

![Agent prompt](images/astro-11-agent-prompt.png)

![Agent run đã được tạo](images/astro-12-agent-created.png)

## 2. Specification gate và chuyển sang làm việc

Mở work item, đọc lại đặc tả do AI đề xuất, chỉnh để có các tiêu chí kiểm chứng cụ thể, sau đó dùng đúng human gate:

| Bước | Trạng thái/kết quả |
|---|---|
| Intake | `ASTRO-1`, type `BUG` |
| Refine | `RAW` → `REFINING` |
| Review | `REFINING` → `REVIEW_REQUIRED` |
| Human verification | Specification version `16` được người dùng xác minh |
| Ready | `REVIEW_REQUIRED` → `READY` |
| Branch plan | Base `develop`, working branch `forgeflow/.../fix/astro-1-...`, base SHA `c0e51646de2dcbed1be061fe00838576a43605b6` |
| Implementation | `READY` → `IN_PROGRESS` |

Không chuyển `READY` trước khi specification quality gate được xác minh. Ảnh:

![Spec được người dùng xác minh](images/astro-04-spec-human-verified.png)

Các trạng thái trung gian và branch-plan snapshots được liệt kê trong [evidence gallery](#evidence-gallery) ở phụ lục.

## 3. Agent thực hiện và xử lý interruption

AgentRun chính:

- id: `fb2f5783-4a9b-4f1f-9731-fe19a6ffa278`;
- provider: `local`;
- agent: `Forgeflow Agent`;
- prompt: bản rút gọn nhưng giữ nguyên acceptance của specification version `16`;
- được approve và start trong UI.

Prompt triển khai thực tế:

```text
Sửa ASTRO-1 trong repository syphune/astro-birth-ai theo đặc tả đã được người review xác minh (spec v16).

Mục tiêu:
- Loại bỏ lỗi production khiến prose relationship thay Anh/anh ấy/Chị/chị ấy thành Người ấy. Đây là đại từ thông thường, không phải participant role.
- Chỉ giữ mapping có cấu trúc role rõ ràng: Người 1 (A) → Bạn, Người 2 (B) → Người ấy, Người A → Bạn, Người B → Người ấy, hoặc token A/B độc lập trong ngữ cảnh relationship.
- Sửa CSS responsive của chat-panel-v2/evidence drawer để viewport mobile 390–412px không bị margin desktop, horizontal overflow hoặc panel lệch khỏi viewport.

Phạm vi kiểm tra:
- Truy vết mọi caller của cleanReaderLanguage và relationshipReaderText để không làm hỏng generic/personal reader.
- Chạy unit tests backend report và frontend report-reading; chạy regression E2E relationship trên desktop và mobile.
- Xác minh các case: prose giữ nguyên “Anh cần nói rõ điều này” và “Chị muốn thêm thời gian”; role A/B vẫn map đúng; evidence drawer và chat phủ đúng viewport, document không có overflow ngang.
- Chỉ thay đổi file cần thiết, không sửa status bằng generic update, không đưa secret hoặc chạy code tùy ý từ repository. Báo cáo diff, commit SHA, lệnh test và kết quả; dừng tại gate review/QA để người chịu trách nhiệm xác nhận.
```

Ảnh approve/start:

![Agent đang thực hiện](images/astro-15-in-progress.png)

Local worker không có output kịp thời nên watchdog đưa run về `INTERRUPTED`. Đây là failure thực tế được ghi nhận trong UI; không coi interruption là hoàn tất. Sau đó run được resume, và một follow-up run riêng được tạo để bổ sung evidence kiểm thử:

- follow-up id: `754e0fd6-83dd-49a0-b047-99d0b8018341`;
- agent: `Astro Fix Agent - QA follow-up`;
- được approve/start trong UI;
- ghi đủ step planning → investigating → implementing → testing;
- kết quả cuối: `COMPLETED`, `3/3` test case PASS.

![Các run trước khi resume](images/astro-20-runs-before-resume.png)

![Agent được resume](images/astro-22-agent-resumed.png)

![Agent hoàn tất](images/astro-30-agent-completed.png)

Các màn hình approve, follow-up và QA checklist đầy đủ nằm trong [evidence gallery](#evidence-gallery).

## 4. Root cause và bản sửa

### Root cause

`cleanReaderLanguage` là helper dùng chung cho nhiều reader surface nhưng chứa lexical replacement quá rộng, ví dụ đổi `Anh ấy`, `anh ấy`, `Chị ấy`, `chị ấy` thành `Người ấy`. `relationshipReaderText` ở frontend cũng lặp lại cùng giả định. Vì vậy một đại từ trong prose bị hiểu nhầm là vai trò participant.

CSS của `.chat-panel-v2` kế thừa width, margin và chiều cao của layout desktop. Ở mobile, panel bị đẩy lệch và có thể làm `documentElement.scrollWidth` lớn hơn viewport.

### Thay đổi tối thiểu

- [`backend/internal/report/friendly.go`](https://github.com/syphune/astro-birth-ai/blob/03a8ce311ac4c143c073c55fe1bcbdf83dc145ef/backend/internal/report/friendly.go): bỏ generic pronoun replacement trong helper dùng chung.
- [`backend/internal/report/friendly_test.go`](https://github.com/syphune/astro-birth-ai/blob/03a8ce311ac4c143c073c55fe1bcbdf83dc145ef/backend/internal/report/friendly_test.go): thêm regression cho `Anh/anh ấy` và `Chị/chị ấy` trong prose.
- [`apps/web/components/report-shell.tsx`](https://github.com/syphune/astro-birth-ai/blob/03a8ce311ac4c143c073c55fe1bcbdf83dc145ef/apps/web/components/report-shell.tsx): relationship mapping chỉ còn label role rõ ràng và token A/B độc lập.
- [`apps/web/components/report-reading.test.tsx`](https://github.com/syphune/astro-birth-ai/blob/03a8ce311ac4c143c073c55fe1bcbdf83dc145ef/apps/web/components/report-reading.test.tsx): thêm regression frontend.
- [`apps/web/app/styles.css`](https://github.com/syphune/astro-birth-ai/blob/03a8ce311ac4c143c073c55fe1bcbdf83dc145ef/apps/web/app/styles.css): mobile reset `width`, `max-width`, `height`, `margin` cho chat panel.
- [`apps/web/e2e/closed-beta.spec.ts`](https://github.com/syphune/astro-birth-ai/blob/03a8ce311ac4c143c073c55fe1bcbdf83dc145ef/apps/web/e2e/closed-beta.spec.ts): thêm E2E relationship desktop/mobile với prose, role mapping và viewport assertions.

Commit đã push lên working branch:

```text
03a8ce311ac4c143c073c55fe1bcbdf83dc145ef
[ASTRO-1] Preserve reader pronouns and fix mobile panels
```

## 5. Review, request changes và resubmit

Forgeflow tạo PR draft thật:

- PR #1: [https://github.com/syphune/astro-birth-ai/pull/1](https://github.com/syphune/astro-birth-ai/pull/1);
- base: `develop`;
- product head ban đầu: `03a8ce311ac4c143c073c55fe1bcbdf83dc145ef`;
- CI/config head cuối: `1b64074c83f91c1eafbc1ae2e3b15ec6f458f989`;
- trạng thái cuối: merged; merge commit `fa3a5d2e3c7ddd59484cbf26451742d7151bc45a`.

Trong review loop, thực hiện `request_changes` để mô phỏng phản hồi reviewer, sau đó chạy lại AgentRun/follow-up, ghi comment tóm tắt diff và test evidence, rồi submit lại code review.

![PR draft được tạo](images/astro-17-pr-created.png)

![Reviewer request changes](images/astro-19-request-changes.png)

Comment review và màn hình resubmit nằm trong [evidence gallery](#evidence-gallery).

Comment đã ghi trong Activity:

```text
Code review ASTRO-1: đã kiểm tra diff 03a8ce311ac4c143c073c55fe1bcbdf83dc145ef. Shared language cleaner giữ nguyên Anh/Chị trong prose; role A/B vẫn map đúng; chat-panel-v2 reset width/margin/height trên mobile. Backend report 367 passed, frontend report-reading 27 passed, full E2E 50 passed và 2 skipped golden mobile.
```

## 6. QA và merge gate

Kết quả kiểm thử sản phẩm:

```text
go test ./internal/report                                      367 passed
npm --workspace @tinhdo/web run test -- report-reading.test.tsx 27 passed
targeted relationship E2E (desktop + mobile)                    2 passed
npm run test:e2e                                                 50 passed, 2 skipped
```

Hai test bị skip là golden mobile snapshot đã có sẵn trong suite; test regression mới không bị skip. Full E2E vẫn có warning pagination có sẵn của `couple_v2` (`reference=37`, `current=36`), không liên quan đến ASTRO-1.

Forgeflow cũng được kiểm tra ở các package bị chạm trong quá trình chạy workflow:

```text
go test ./internal/agentrun ./internal/autonomous ./internal/github ./internal/workitem
65 passed in 4 packages
```

Kiểm thử toàn hệ thống tại Forgeflow cũng đã pass:

```text
make test              backend race tests + desktop/web unit tests: PASS
make test-integration  PostgreSQL integration suite: PASS
make lint              go vet + web lint: PASS
make typecheck         web + desktop TypeScript: PASS
make build             backend + Next production build: PASS
make security          govulncheck: 0 vulnerabilities affecting code: PASS
make smoke             backend test + all command builds: PASS
```

Khi bấm `Move to QA`, hệ thống từ chối vì GitHub chưa có successful CI cho head SHA. Đây là kết quả đúng của policy, được chụp lại:

![QA bị chặn bởi CI gate](images/astro-33-qa-gate-blocked.png)

Đã kiểm tra read-only trên GitHub và ghi lại các lần chạy:

- PR cuối `mergeable=true`, `draft=false`, base `develop`, head `1b64074c83f91c1eafbc1ae2e3b15ec6f458f989`;
- CI đầu tiên sau khi submit bị lỗi quyền đọc commit của PR; thêm `pull-requests: read`;
- CI kế tiếp chạy được gitleaks nhưng checkout shallow không có commit base `03a8ce3...`; thêm `fetch-depth: 0` ở commit `b5122f4d...`;
- bundle budget đã dùng `rg --files` không ổn định với asset trong `.next/`; đổi sang `find` ở commit `dc2fee9...` sau khi contract test local pass;
- astrology contract test phụ thuộc kernel thật tại `/app/kernels/de440.bsp`, trong khi kernel được cố ý loại khỏi Git; đổi test sang temporary checksum fixture ở commit `f70d1f6...`, giữ nguyên kiểm tra fail-closed và matching checksum;
- compose validation dùng `--no-interpolate` làm volume có `${...:?}` không parse được; cấp fixture env/path tạm cho cả hai compose stack ở commit `a5591ca...`, vẫn giữ image build đầy đủ;
- run `22` đã chạy toàn bộ gate nhưng migration smoke test lộ assertion không portable: PostgreSQL trả `true:ENABLE PAID LAUNCH GATE PRODUCTION`, còn workflow kỳ vọng `t:...`; thêm output assertion ở `f7b4b9e`, chuẩn hóa boolean và kỳ vọng đúng ở `1b64074`;
- run `23` [đã pass](https://github.com/syphune/astro-birth-ai/actions/runs/32999952510) trên đúng head `1b64074`: cả 23 step, gồm browser acceptance, compose image builds, migration/seed và V4 backward/forward smoke test, đều `success`;
- sau khi xác minh CI, PR được merge bằng SHA pin; Forgeflow nhận evidence qua transition service và hoàn tất QA/human verification.

Lần đầu chưa tự chuyển `QA/DONE` hoặc merge khi gate còn đang chạy. Sau khi gate xanh, quy trình đã hoàn tất bằng transition service và optimistic version: request lặp với version cũ trả `409 stale version`, còn `v8 → v9 → v10` thành công.

Audit activity cuối xác nhận chuỗi version thực tế: `RAW v1 → REFINING v2 → REVIEW_REQUIRED v3 → READY v4 → IN_PROGRESS v5 → CODE_REVIEW v6 → IN_PROGRESS v7 → CODE_REVIEW v8 → QA v9 → DONE v10`.

## 7. Remediation P0: release governance cho pilot công ty

Sau khi rà soát, Forgeflow đã bổ sung một template **release-gated** tùy chọn theo Project; ASTRO-1 không bị đổi lịch sử sang template này. Project manager cấu hình tại **Project settings → AI autonomous → Delivery policy** rồi cài workflow release nếu cần:

~~~text
QA → READY_FOR_RELEASE → DEPLOYING → PRODUCTION_VERIFY → DONE
                         ↘ ROLLED_BACK khi release/deploy/verify thất bại
~~~

Các control đã có ở server/API:

- PR và CI được kiểm tra trên đúng head SHA hiện tại; required check được đối chiếu theo tên, không chấp nhận một CI run “xanh” bất kỳ.
- Review có minimum independent approvals, loại approval của author, required project roles và path rules như `payments/** → security_reviewer`.
- Work Item có risk `LOW` / `NORMAL` / `HIGH` / `CRITICAL`; policy có thể tăng approvals, bắt buộc QA, rollback plan hoặc production verification theo risk.
- Regression case có `REQUIRED` / `OPTIONAL` / `INFORMATIONAL`; chỉ case `REQUIRED` phải `PASS` trên đúng commit của run. Ở release gate, PR phải đã approved và merged.
- Production approval và production verification là capability human-only, tách khỏi PR author/reviewer. Failure ở release, deployment hoặc verification đi qua `ROLLED_BACK` và rollback plan human-verified.
- Hotfix snapshot từ production, deploy/verify xong mới tạo sync-backport về development; terminal `DONE` bị chặn cho đến khi webhook ghi nhận sync PR `MERGED`.
- Agent server chạy Docker với workspace riêng, root filesystem read-only, non-root UID, drop capabilities, `no-new-privileges`, `/tmp` `noexec,nosuid`, CPU/RAM/PID/timeout bounded và output redaction. Network mặc định `none`; bật `bridge` chỉ trong môi trường có egress allowlist khi provider cần gọi API. Docker socket có quyền gần tương đương root nên runner production phải ở host/VM hoặc daemon rootless riêng.

Các gate mới được kiểm tra bằng unit/contract tests cho current-head CI, PR author/path-role SoD, required QA case và exact commit, release workflow/rollback, deployment approval/verification, hotfix sync và runner container hardening. Đây là bằng chứng regression của implementation; chưa phải số đo capacity production.

### Pilot rollout đề xuất

Trong 2–4 tuần đầu, bật release-gated cho một Project, 3–8 developer và khoảng 50–100 Work Item. Không cho autonomous production deployment; merge production và verify vẫn do người có quyền thực hiện. Theo dõi lead time, thời gian chờ refinement/review/QA/release, tỷ lệ QA failure/follow-up/escaped defect, AgentRun interruption/success, số lần bypass và duplicate webhook/transition.

Các số liệu hard E2E ở trên (`400` request local, concurrency `32`, browser p95/p99) chỉ là regression/performance baseline của release trước. Cần chạy thêm 50–100 user đồng thời, 20–50 AgentRun, webhook burst, worker/API restart, PostgreSQL pool exhaustion, GitHub/LLM timeout/outage và kiểm tra duplicate transition/PR trước khi kết luận hệ thống chịu tải production.

## 8. Sau khi hoàn tất: vòng đời giống Jira

Trong lần rà soát bổ sung, vấn đề “task đã hoàn tất nhưng vẫn nằm ở Backlog” được xử lý như một user journey riêng. Prompt dùng để giao cho agent mô tả rõ hành vi cần có:

```text
Rà soát vòng đời work item giống Jira trong Forgeflow. Khi item đi vào terminal
status, hãy ghi completed_at đúng lúc transition; không dùng updated_at vì sửa
title hoặc metadata sau đó không được gia hạn thời gian lưu trên Board.

Backlog mặc định chỉ hiển thị item đang xử lý. Board vẫn có cột DONE và cho phép
project manager chọn Never, 7, 14, 30 hoặc 60 ngày để ẩn card Done cũ. Item bị
ẩn khỏi Board vẫn phải tìm được bằng filter status DONE/terminal. Kiểm tra cả
transition service, Board drag-and-drop, PostgreSQL, API contract, UI settings,
mobile viewport và không để request hợp lệ rơi vào HTTP 500.
```

Kết quả người dùng quan sát được:

- item hoàn tất rời khỏi danh sách Backlog mặc định nhưng không bị xóa;
- Board vẫn hiển thị cột `DONE`, và card Done mới vẫn nhìn thấy;
- khi chọn `14` ngày trong **Project settings → Board**, card Done cũ biến khỏi Board;
- item vẫn xuất hiện khi chọn filter terminal; sửa metadata không reset `completed_at`;
- nếu kéo item vào terminal status từ Board, server cũng ghi đúng `completed_at`.
- các cột Board giữ trên một hàng cuộn ngang, nên workflow nhiều trạng thái không bị vỡ thành nhiều hàng hoặc làm tràn trang mobile.

Ảnh bằng chứng: [Board sau khi áp dụng retention](images/forgeflow-board-retention.png). Test browser còn kiểm tra toàn bộ route matrix ở desktop/mobile; test backend kiểm tra transition, retention cutoff, tenant settings và PostgreSQL round-trip.

## Technical appendix: How Forgeflow was validated end-to-end

Phần đầu là câu chuyện public của ASTRO-1. Phần dưới là bằng chứng kỹ thuật đầy đủ cho release surface: lỗi nền tảng phát hiện qua E2E, user journey, contract boundary, performance và giới hạn đo. Các link source code dùng permalink cố định; link artifact giữ dạng monorepo-relative để có thể mở trực tiếp trong workspace.

### A. Các lỗi nền tảng phát hiện trong lúc E2E

E2E end-to-end đã phát hiện và sửa các lỗi của Forgeflow, không chỉ lỗi Astro:

- query PR/CI evidence dùng `wi.key`, trong schema key nằm ở project; đã join project và dựng key đúng;
- một số PostgreSQL query truyền UUID vào placeholder text mà thiếu cast `::uuid`, làm trang work item/agent run trả 500;
- `ResultInput` thiếu JSON tags nên `commit_sha`/metadata từ HTTP không bind đúng;
- attachment và custom-field ghi audit với UUID rỗng, làm thao tác hợp lệ rơi vào PostgreSQL `22P02`; đã cấp ID audit trước khi ghi;
- `custom-field PATCH`, environment/deployment và autonomous retry nhận payload snake_case theo OpenAPI nhưng struct thiếu JSON tags; đã khớp lại input contract;
- environment/deployment và autonomous feedback truyền giá trị tuỳ chọn thành `NULL` vào cột `NOT NULL`; đã chuẩn hoá default theo schema;
- resume một AgentRun đã approve nhưng bị interrupted cần được phép sau khi work item đã sang `IN_PROGRESS`;
- mobile app shell giữ min-content của workflow strip, làm content rộng khoảng 800px ở viewport 390px; đã thêm `min-width: 0`, cho tabs xuống dòng và xếp transition control full-width để không còn control bị cắt;
- header/route ID không hợp lệ đi thẳng vào PostgreSQL UUID có thể thành 500; lớp error mapping SQLSTATE `22P02` nay trả `422 INVALID_ARGUMENT` an toàn, không lộ chi tiết database;
- các sửa này có test package-level tương ứng và không dùng generic update để lách status transition.

Đây là lý do nên chạy cả happy path lẫn failure/recovery path trong E2E: nếu chỉ kiểm tra “tạo task → DONE”, các lỗi persistence, gate và contract sẽ bị che khuất.

### B. Quy trình để người dùng tự lặp lại

1. Chọn project Astro và repository đã link.
2. Tạo `BUG`, mô tả symptom, non-goals, acceptance cases và prompt agent như ở trên.
3. Review specification; chỉ khi đúng mới verify và chuyển `REVIEW_REQUIRED → READY`.
4. Prepare branch plan; kiểm tra base SHA, working branch và repository.
5. Start work; tạo/approve/start AgentRun. Nếu watchdog interrupt, đọc lỗi và resume/follow-up, không tự coi là pass.
6. Kiểm tra diff, commit SHA, unit test, frontend test và E2E desktop/mobile.
7. Ghi test result vào AgentRun; reviewer comment vào Activity; dùng request changes nếu cần.
8. Tạo PR draft và submit code review.
9. Chờ reviewer gate + successful CI. Không dùng API generic hoặc sửa database để bypass gate.
10. Chuyển sang QA, xác minh branch sync/human verification, complete rồi mới merge.

Ảnh trong thư mục [`images`](images) là bằng chứng theo từng chặng của case study này.

### C. Full-system E2E: định nghĩa đúng mẫu số

Route matrix chỉ là một lát cắt. Coverage chính được tính theo kết quả mà người dùng có thể hoàn thành hoặc quan sát được; các boundary bên dưới là bằng chứng để không bỏ sót process phía sau UI.

#### User-facing coverage

Mỗi dòng là một outcome người dùng độc lập, tính một lần. Một outcome được xem là đạt khi browser journey hoặc acceptance thật cho thấy người dùng có thể hoàn thành thao tác, thấy đúng guard quyền, hoặc đi qua failure/recovery có chủ đích.

| User-facing outcome | Bằng chứng | Kết quả |
|---|---|---:|
| Đăng nhập, giữ session, đăng xuất và quay về auth gate | `app-v2.spec.ts`, auth conformance | 1/1 |
| Tạo organization, chọn organization/workspace/project đúng scope | `app-v2.spec.ts` | 1/1 |
| Tạo project và thấy đúng quyền create/read-only | `app-v2.spec.ts` | 1/1 |
| Mở backlog, tìm kiếm, đổi list/board và mở detail drawer | `app-v2.spec.ts` | 1/1 |
| Tạo BUG từ intake với title/context | `full-system.spec.ts` | 1/1 |
| Kết nối, link repository và đọc engineering context | `app-v2.spec.ts`, `full-system.spec.ts` | 1/1 |
| Viết specification, chọn code context và human verification | `app-v2.spec.ts` | 1/1 |
| Xem responsibility, assign người xử lý và ghi comment | `full-system.spec.ts` | 1/1 |
| Đi qua workflow review loop và nhánh QA không đạt/recovery | `full-system.spec.ts` | 1/1 |
| Tạo, approve, start, resume AgentRun và ghi evidence review | live Astro flow, `full-system.spec.ts` | 1/1 |
| Bắt đầu, xem feedback, retry và cancel autonomous workflow | live Astro flow, `full-system.spec.ts` | 1/1 |
| Preview/prepare branch, tạo PR nháp và prepare sync | `full-system.spec.ts` | 1/1 |
| Tạo, sửa và chuyển vòng đời sprint | `full-system.spec.ts`, planning tests | 1/1 |
| Cấu hình workflow, branch policy, AI policy và environment | `full-system.spec.ts` | 1/1 |
| Quản lý project people, custom fields và automation rules | `full-system.spec.ts` | 1/1 |
| Quản lý organization/workspace settings | `full-system.spec.ts` | 1/1 |
| Đọc và đánh dấu notification trong Inbox | `full-system.spec.ts` | 1/1 |
| Tạo/revoke PAT và tạo/copy cấu hình MCP | `full-system.spec.ts` | 1/1 |
| Dùng Desktop Runner cho worktree, diff và agent execution | desktop agent/repository tests | 1/1 |
| Dùng MCP client để đọc context và gọi capability | official MCP stdio E2E | 1/1 |
| Nhìn UI responsive, theme, loading/error/read-only states | browser suite + Astro relationship E2E | 1/1 |
| Xác nhận bug Astro: prose giữ `Anh/Chị`, role mapping đúng, mobile không overflow | Astro product E2E + CI run 23 | 1/1 |
| Cấu hình Board retention: Done vẫn ở cột, card cũ ẩn theo `completed_at`, terminal filter vẫn tìm được | `full-system.spec.ts`, board/workitem/tenant tests | 1/1 |
| **Tổng user-facing outcome chính** | **23 hành trình/capability** | **23 outcome đạt** |

| Boundary | Mẫu số | Bằng chứng | Kết quả |
|---|---:|---|---:|
| Web app | 18 route variant trên 12 page entrypoint, kiểm tra desktop và mobile 390px | [`full-system.spec.ts`](../../apps/web/e2e/full-system.spec.ts), browser Chromium | 18/18 desktop + 18/18 mobile đạt |
| Public HTTP API | 157 OpenAPI operation | `TestAPIContractSurface` dispatch 155 operation, OAuth start/callback có auth conformance test riêng; hard E2E historical đã dispatch 155 trên API thật và chặn HTTP 500/unsafe 5xx | 157/157 pass |
| API health | 2 endpoint: `/health/live`, `/health/ready` | `TestAPIHealthSurface` | 2/2 pass |
| MCP stdio | 39 tool + 1 static resource + 4 resource template | official MCP client; gọi từng tool, đọc skill và đọc dynamic template resource | 44/44 pass |
| Desktop bridge | 17 IPC method/channel | [`preload/index.test.ts`](../../apps/desktop/src/preload/index.test.ts) và tests agent/repository/server | 17/17 pass |
| Runner service | 2 endpoint: health + authenticated job | [`cmd/runner/http_test.go`](../../backend/cmd/runner/http_test.go) và dispatcher test | 2/2 pass |
| Worker lifecycle | 2 nhánh rủi ro cao: duplicate automation + runner unavailable | [`cmd/worker/main_test.go`](../../backend/cmd/worker/main_test.go) | 2/2 pass |
| **Composite system surface** | **239 checks trước khi thêm Board settings** | **Tổng các boundary ở trên** | **239/239 checks pass; Board retention bổ sung có test riêng** |

> `157/157` của HTTP hiện tại là phép ghép có kiểm soát: test contract dispatch 155 operation vì composition root memory không cấu hình hai OAuth operation; hai operation còn lại được kiểm tra bằng `auth` package với state cookie/callback validation. Hard E2E `155/155` là số liệu của release trước. Đây là lý do không gộp test “route trả 404 envelope” với test hành vi OAuth vào một con số không giải thích được.

#### Web user journey

Suite browser chạy Next app thật, chỉ stub ở HTTP boundary `/api/v1` bằng stateful mock để cô lập dữ liệu test; nó vẫn assert UI, request method/path, mutation state và workflow transition key. PostgreSQL không bị thay thế bởi mock: backend race/integration/smoke chạy riêng.

Ma trận route:

| Nhóm | Route đã kiểm tra | Kết quả |
|---|---:|---:|
| App shell, backlog, planning, repositories, work item detail | 5/5 | pass |
| Project settings: workflow, Board, branching, autonomous, people, fields, automation | 7/7 | pass |
| Organization settings: general, members | 2/2 | pass |
| Workspace settings: general, members | 2/2 | pass |
| Inbox, developer account | 2/2 | pass |
| **Tổng ma trận route người dùng** | **18/18** | **pass** |

Năm kịch bản browser bao phủ hành vi chính:

1. Route matrix: mở đủ 18 route và assert heading, authorization, Board settings, branch policy, automation rule, token API.
2. Work delivery: tạo work item, board keyboard move, assignment, comment, specification, test review note, autonomous feedback/retry, branch preview/prepare, PR draft và sync operation. Workflow được đi qua cả nhánh sửa code và QA không đạt: `CODE_REVIEW → IN_PROGRESS`, `QA → IN_PROGRESS`, rồi mới `QA → DONE`.
3. Agent/autonomous control: tạo, approve, start, resume AgentRun; bắt đầu và cancel autonomous workflow.
4. Planning/admin: sprint create/edit/start/complete, repository context, workflow/branch/AI policy, environment, project member role/remove, custom field, automation rule, organization member, workspace rename, inbox read/read-all, developer token và MCP connection create/copy/revoke.
5. Responsive/no-500 audit: mở đủ 18 route ở viewport 390px, kiểm tra không overflow ngang, không control tương tác bị cắt và không response HTTP `>=500`; chụp lại trạng thái mobile.

Ảnh sinh ra từ browser run:

Các ảnh của full-system run, MCP/desktop và mobile audit nằm trong [evidence gallery](#evidence-gallery).

Ma trận release trước chạy trên bản web production standalone nối API PostgreSQL thật đạt `34/34` (17 route desktop + 17 route mobile), không có document `>=500`, overflow ngang hoặc control tương tác bị cắt. Artifact có [report](../../artifacts/live-production-route-matrix/report.md), [JSON](../../artifacts/live-production-route-matrix/report.json) và [ảnh desktop](../../artifacts/live-production-route-matrix/forgeflow-live-desktop.png)/[ảnh mobile](../../artifacts/live-production-route-matrix/forgeflow-live-mobile.png). Bản browser bổ sung hiện tại đạt `18/18` route desktop + `18/18` route mobile và bao gồm Board settings.

#### Các entrypoint ngoài browser

| Process | Cách xác minh |
|---|---|
| `cmd/api` | API contract/health surface, middleware security tests và production build |
| `cmd/worker` | automation idempotency và autonomous dispatch failure path; lần test đầu phát hiện child AgentRun bị kẹt `PREPARING`, sau đó sửa để cùng fail về `FAILED` với parent feedback |
| `cmd/mcp` | build server, official MCP stdio client initialize/list/read/call toàn bộ 39 tool |
| `cmd/mcp-bridge` | bridge proxy test cho tools, resource và resource template |
| `cmd/migrate` | PostgreSQL integration migrate-up trên toàn bộ migration chain |
| `cmd/runner` | health, auth rejection, malformed JSON và authorized job dispatch |
| Desktop | preload contract đủ 17 method; agent-run, Git, repository server client có unit tests và input validation |

#### Product Astro là acceptance thật

Forgeflow system coverage không thay thế acceptance của repository Astro. Trên branch/PR thật, các gate sản phẩm vẫn pass:

```text
backend report tests                         367 passed
frontend report-reading tests                 27 passed
targeted relationship E2E (desktop + mobile)  2 passed
full Astro product E2E                        50 passed, 2 skipped (golden snapshot có sẵn)
GitHub CI run 23                              23/23 steps success
```

Lệnh tái chạy local Forgeflow:

```bash
rtk make test
rtk env FORGEFLOW_TEST_POSTGRES_PORT=55433 make test-integration
rtk env PLAYWRIGHT_PORT=13001 make test-e2e
rtk make lint
rtk make typecheck
rtk make build
rtk make security
rtk make smoke
```

`scripts/test-integration.sh` nhận `FORGEFLOW_TEST_POSTGRES_PORT` để không đụng PostgreSQL của Astro đang chạy; compose test vẫn dùng project riêng và cleanup tự động.

#### Hard E2E và non-functional release run

Ngoài browser journey, chạy `scripts/hard-e2e.mjs` trên API database-backed thật. Harness đọc trực tiếp OpenAPI nên không phụ thuộc danh sách route viết tay:

- lần hard run release trước gọi đủ `155/155` operation, kiểm tra error envelope và fail ngay khi gặp HTTP 500 hoặc 5xx không được bọc an toàn;
- chạy thêm functional journey với payload hợp lệ: `138` thao tác Astro thật, gồm intake, specification gate, review/QA, sprint, automation, deployment, AgentRun, autonomous recovery, tenant/PAT, notification, audit và MCP; không có 4xx/5xx hoặc network error;
- benchmark `400` request concurrent trên health, `/me`, organizations và projects với concurrency `32`;
- mở web standalone image thật qua đủ `17` route ở desktop `1280x900` và mobile `390x844`, `3` vòng (`102` navigation), đo navigation timing, long task, heap, overflow và response 5xx;
- lấy RSS/CPU API process bằng `ps` và web container bằng `docker stats`.

Kết quả lần chạy release ngày 2026-08-27:

| Chỉ số | Kết quả | Ngưỡng harness |
|---|---:|---:|
| OpenAPI operations | 155/155; HTTP 500: 0; unwrapped 5xx: 0 | 0 lỗi |
| Functional Astro journey | 138 thao tác; HTTP 500: 0; any 5xx: 0; network: 0 | 0 lỗi |
| API load | p95 `24.9ms`; p99 `37.0ms`; 400 mẫu, concurrency 32 | p95 ≤ `750ms`; p99 ≤ `1500ms` |
| Browser navigation | p95 `282.0ms`; p99 `420.8ms`; 102 mẫu | p95 ≤ `5000ms`; p99 ≤ `8000ms` |
| Browser heap delta sau GC | `2.8MB` | ≤ `64MB` |
| API process | RSS max `37.9MB`; CPU avg/max `0%/0%`; CPU time `0.06s` | đo thực tế |
| Web standalone container | RSS max `91.7MB`; CPU avg/max `31.6%/66.6%` | đo thực tế |

Surface có `2` response `503` có envelope ở GitHub user OAuth vì callback không có session hợp lệ trong hard probe; đó là dependency/auth-gated behavior có chủ đích, không phải 500. Repository integration và các route GitHub của Astro đã chạy 200 với cấu hình App trong functional/browser run. MCP trả `400 text/plain` cho JSON-RPC rỗng theo protocol; harness phân biệt riêng response này với lỗi HTTP nội bộ.

Artifact tái kiểm tra: hard run [report.md](../../artifacts/hard-e2e-full-live-configured/report.md), [report.json](../../artifacts/hard-e2e-full-live-configured/report.json), [ảnh desktop](../../artifacts/hard-e2e-full-live-configured/forgeflow-live-desktop.png), [ảnh mobile](../../artifacts/hard-e2e-full-live-configured/forgeflow-live-mobile.png); functional journey [report.md](../../artifacts/functional-e2e-astro-final/report.md), [report.json](../../artifacts/functional-e2e-astro-final/report.json). Harness archive work item test, cancel execution, revoke token, remove member và xoá các resource có endpoint cleanup; environment test và sprint đã complete được giữ lại có chủ đích trong local fixture DB vì API hiện không có delete environment và invariant không cho xoá sprint đã complete.

Lệnh chạy lại:

```bash
rtk env FORGEFLOW_PERF_API_URL=http://127.0.0.1:18082 \
  FORGEFLOW_PERF_WEB_URL=http://127.0.0.1:13004 \
  FORGEFLOW_PERF_ORGANIZATION_ID=582554ef-120d-44cf-89cb-2db3b959d689 \
  FORGEFLOW_PERF_ACTOR_ID=37aa4a1b-e5e1-40cc-b036-23e0922f0ba5 \
  FORGEFLOW_PERF_WORKSPACE_ID=1e3f9209-e7aa-412e-a701-602a9693d5c1 \
  FORGEFLOW_PERF_PROJECT_ID=e7a275e7-ad39-480e-af26-f1d60278be8e \
  FORGEFLOW_PERF_WORK_ITEM_ID=475e73eb-5309-4ec8-b5db-01553634d8bf \
  FORGEFLOW_PERF_ITERATIONS=100 \
  FORGEFLOW_PERF_CONCURRENCY=32 \
  FORGEFLOW_PERF_UNIQUE_RATE_KEYS=true \
  FORGEFLOW_PERF_UNIQUE_BROWSER_RATE_KEYS=true \
  FORGEFLOW_PERF_WEB_REPETITIONS=3 \
  FORGEFLOW_PERF_WEB_VIEWPORTS='desktop:1280x900,mobile:390x844' \
  FORGEFLOW_PERF_WEB_PATHS=all \
  FORGEFLOW_PERF_WEB_CONTAINER=forgeflow-web-live-hard \
  FORGEFLOW_PERF_AUTHORIZATION='Bearer hard-e2e-full-live-configured' \
  FORGEFLOW_PERF_OUTPUT_DIR=artifacts/hard-e2e-full-live-configured \
  node scripts/hard-e2e.mjs
rtk make functional-e2e
rtk env PLAYWRIGHT_PORT=13001 make test-e2e-torture
```

`test-e2e-torture` lặp 5 kịch bản browser stateful ba lần, tổng `15/15` pass trong `1.2m`. Các số resource là phép đo một máy local với API process và standalone web container; chúng chứng minh regression/performance budget hiện tại, không thay thế load test nhiều instance hoặc capacity plan production.

### D. Độ phủ trên 95% và giới hạn đo

Theo mẫu số user-facing, cả 23 outcome đều đạt, vượt ngưỡng `>95%` ở cấp hành trình người dùng. Browser hiện tại đạt `18/18 route desktop + 18/18 route mobile`, `5/5 full-system scenario`, `13/13 browser test`; torture run historical lặp `15/15`; functional journey historical đạt `138` thao tác thật không lỗi; API contract hiện tại đạt `157/157` operation khi ghép auth conformance, còn các contract MCP/desktop/runner/worker giữ mức `239/239 checks` của release trước. No-500 audit đạt ở browser journey và public API contract; UI audit không phát hiện overflow ngang hoặc control tương tác bị cắt. Non-functional run đạt p95/p99 và heap budget đã nêu ở trên.

Đây là coverage release surface, không phải line coverage. Đo độc lập bằng:

```bash
rtk go test -coverprofile=/tmp/forgeflow-cover.out ./...
rtk go tool cover -func=/tmp/forgeflow-cover.out | tail -n 1
```

Kết quả Go backend hiện tại là **37.0% statements**. Không dùng surface coverage để tuyên bố code coverage; nếu yêu cầu sau này là >95% line coverage thì đó là một mục tiêu khác, cần mở rộng unit/property tests cho implementation chưa chạy, không thể đạt trung thực chỉ bằng thêm route smoke.

### E. Checklist release thực tế

- [x] Bug được tạo qua intake/agent với acceptance cụ thể.
- [x] Specification được người dùng xác minh trước `READY`.
- [x] AgentRun có approve, interruption/recovery và QA evidence.
- [x] Code review có request changes và resubmit.
- [x] Full-system browser matrix đạt 18/18 route và đi qua các nhánh workflow/recovery.
- [x] Board retention giống Jira: Backlog lọc terminal, Board giữ Done, setting 14 ngày ẩn card cũ, terminal filter vẫn tra cứu được.
- [x] Browser torture run lặp 3 lần: 15/15 test pass.
- [x] Browser audit desktop/mobile đạt 18/18 route mỗi viewport; không có response `>=500`, overflow ngang hoặc control tương tác bị cắt.
- [x] API contract hiện tại 157/157 khi ghép OAuth conformance; hard API release trước 155/155 không có HTTP 500 hoặc 5xx không có error envelope.
- [x] Functional Astro journey 138 thao tác payload hợp lệ không có 4xx/5xx hoặc network error.
- [x] Hard non-functional run đạt API/browser p95-p99, heap delta và ghi nhận RSS/CPU.
- [x] Public HTTP API đạt 157/157 operation khi ghép contract dispatch và OAuth conformance.
- [x] MCP, desktop IPC, runner và worker boundary đều có test contract/lifecycle tương ứng.
- [x] Unit, integration, build, lint, security và smoke local đều pass.
- [x] GitHub CI xanh trên đúng head SHA `1b64074` (run 23, 23/23 steps).
- [x] Forgeflow nhận CI/branch evidence và chuyển `CODE_REVIEW v8 → QA v9 → DONE v10` bằng transition service.
- [x] Merge sau khi các gate được xác minh: PR #1 merge commit `fa3a5d2`.

### Evidence gallery

Phần story chỉ nhúng các ảnh cần để theo dõi câu chuyện. Các ảnh trạng thái còn lại được giữ dưới dạng link để người đọc có thể kiểm tra từng bước mà không làm loãng nội dung chính.

**Intake và specification**

- [Spec draft](images/astro-02-spec-draft.png)
- [Spec đã lưu](images/astro-03-spec-saved.png)
- [Review required](images/astro-05-review-required.png)
- [Spec reviewed](images/astro-06-spec-reviewed.png)
- [Ready](images/astro-07-ready.png)
- [Branch preview](images/astro-08-branch-preview.png)
- [Branch plan đã chuẩn bị](images/astro-09-branch-prepared.png)
- [Branch plan ready](images/astro-10-branch-ready.png)

**Agent, interruption và QA**

- [Agent resumed snapshot](images/astro-11-agent-resumed.png)
- [Agent được approve](images/astro-13-agent-approved.png)
- [Agent bắt đầu](images/astro-14-agent-started.png)
- [Autonomous workflow resumed](images/astro-16-autonomous-resumed.png)
- [Autonomous workflow executing](images/astro-21-autonomous-executing.png)
- [Follow-up run được tạo](images/astro-23-agent-followup-created.png)
- [Follow-up được approve](images/astro-24-followup-approved.png)
- [Follow-up bắt đầu](images/astro-25-followup-started.png)
- [QA checklist](images/astro-26-qa-checklist-open.png)
- [Agent resumed sau watchdog](images/astro-28-agent-resumed-after-watchdog.png)
- [QA results 3/3](images/astro-29-qa-results.png)

**Review và submit**

- [Code review lần đầu](images/astro-18-code-review.png)
- [Comment code review](images/astro-31-code-review-comment.png)
- [Submit lại code review](images/astro-32-code-review-resubmitted.png)

**Full-system và responsive audit**

- [Full-system branch plan](images/forgeflow-full-e2e-branch-plan.png)
- [Full-system developer/MCP](images/forgeflow-full-e2e-developer.png)
- [User-controlled AgentRun và autonomous lifecycle](images/forgeflow-user-agent-lifecycle.png)
- [Board retention sau khi chọn 14 ngày](images/forgeflow-board-retention.png)
- [Mobile usability audit](images/forgeflow-mobile-usability.png)
- [Live desktop capture](images/forgeflow-live-desktop.png)
- [Live mobile capture](images/forgeflow-live-mobile.png)

## Ghi chú môi trường

Browser MCP không có browser session khả dụng trong môi trường test, nên luồng UI được chạy bằng Playwright chính thức của project với local Forgeflow API/web và GitHub App read-only verification. Các artifact untracked có sẵn trong repository Astro (`.DS_Store`, `.playwright-mcp`, `-`) không bị sửa hay xóa.
