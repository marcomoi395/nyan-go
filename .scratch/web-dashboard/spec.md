Status: ready-for-agent

# Web dashboard SSR cho sổ thu chi cá nhân

## Problem Statement

Bot Discord đã lưu, tìm kiếm, xóa mềm và thống kê giao dịch trong SQLite, nhưng việc xem lịch sử và so sánh thu chi dài hạn không phù hợp với giao diện hội thoại. Người dùng cần một web app cá nhân, tối giản, ưu tiên SSR để xem danh sách, thêm, xóa, tìm kiếm và tạo báo cáo trên cùng nguồn dữ liệu.

Feature này bổ sung một adapter web; không thay thế bot Discord và không thay đổi spec lịch sử của bot.

## Solution

Chạy một HTTP server trong cùng binary với bot. Server dùng `net/http`, `html/template` và CSS tĩnh; không thêm framework, SPA, API JSON, ORM hoặc thư viện biểu đồ. Web dùng `LedgerService`, SQLite và scope Discord đã cấu hình. Mỗi mutation web có source ID ngẫu nhiên duy nhất dạng `web:<random-id>` để giữ audit và idempotency.

Các route SSR:

- `/` hiển thị tổng quan tháng hiện tại.
- `/transactions` hiển thị danh sách, bộ lọc và phân trang.
- `/transactions/new` hiển thị form thêm một giao dịch.
- `/transactions/{id}/delete` hiển thị xác nhận và thực hiện xóa mềm.
- `/reports` hiển thị báo cáo khoảng thời gian và so sánh kỳ trước.
- `/assets/app.css` phục vụ stylesheet nhúng trong binary.

## User Stories

1. Là người dùng cá nhân, tôi muốn xem giao dịch mới nhất theo trang để toàn bộ lịch sử vẫn truy cập được.
2. Tôi muốn lọc theo ngày, loại, danh mục, ghi chú và khoảng tiền bằng query string có thể bookmark.
3. Tôi muốn thêm một giao dịch bằng form có validation, danh mục phù hợp loại giao dịch và thời điểm mặc định hiện tại.
4. Tôi muốn xem tóm tắt giao dịch trước khi xác nhận xóa để tránh thao tác nhầm.
5. Tôi muốn mọi xóa là soft-delete có audit như thao tác Discord.
6. Tôi muốn xem tổng thu, tổng chi, số dư, thay đổi so với kỳ trước, phân bổ danh mục và xu hướng theo ngày.
7. Tôi muốn giao diện dễ đọc trên desktop lẫn điện thoại, dùng được bằng bàn phím và không phụ thuộc JavaScript.

## Implementation Decisions

- **Runtime:** Một process, một SQLite connection owner, một lifecycle shutdown. HTTP server khởi động sau khi cấu hình, store và application service hợp lệ.
- **Stack:** Standard library `net/http` + `html/template`; assets nhúng bằng `embed`. Không dùng Go web framework vì routing và SSR hiện tại không cần nó.
- **Access:** Mặc định `WEB_ADDR=127.0.0.1:8080`. Truy cập điện thoại qua Tailscale hoặc reverse proxy tin cậy. Không cung cấp app login, public binding hoặc public JSON API trong v1.
- **Identity:** Web luôn dùng `DISCORD_USER_ID`, `DISCORD_GUILD_ID`, `DISCORD_CHANNEL_ID` từ config. Browser không được cung cấp hoặc ghi đè scope.
- **Mutation safety:** Mọi form ghi dùng POST, CSRF token ngẫu nhiên trong cookie `SameSite=Strict` và hidden field, kiểm tra request không phải cross-site, rồi áp dụng Post/Redirect/Get.
- **Create:** Form nhận type, integer VND amount, category, note và local datetime. Cho phép backdate; validation domain vẫn là nguồn quyết định cuối.
- **Delete:** GET chỉ hiển thị confirmation; POST mới soft-delete. Không có trash/restore UI v1.
- **Search:** Thêm `Limit`/`Offset` vào filter nội bộ. Web dùng 25 dòng/trang; Discord giữ default 20. Tổng số kết quả và trạng thái trang luôn hiển thị.
- **Reports:** Mặc định tháng hiện tại so với tháng trước. Cho phép khoảng ngày tùy chỉnh. Hiển thị cards, bảng so sánh, thanh tỷ trọng CSS và xu hướng ngày; không dùng chart library.
- **Timezone:** Mọi boundary và grouping báo cáo là `Asia/Ho_Chi_Minh`. Group theo ngày không được cắt chuỗi UTC.
- **UI:** Notion-inspired, không sao chép: nền giấy ấm, typography rõ, sidebar desktop, navigation compact trên mobile, màu thu/chi tiết chế, focus visible và semantic HTML.
- **Failure behavior:** Validation hiển thị cạnh form với dữ liệu người dùng đã nhập. Lỗi nội bộ được log chi tiết nhưng trả thông báo chung; không lộ note hay secrets trong log.
- **Deployment:** Container bind `0.0.0.0:8080` bên trong nhưng compose chỉ publish `127.0.0.1:8080`. Tailscale/reverse proxy nằm ngoài scope code.

## Acceptance Criteria

- `go test ./...` xanh; web handler được test bằng `httptest` không cần Discord hay network.
- Danh sách có phân trang 25 dòng, giữ filters qua links và không hiển thị soft-deleted rows.
- Form thêm ghi đúng scope cấu hình, source ID duy nhất và audit hiện có.
- Cross-site hoặc thiếu/sai CSRF token không thể tạo hay xóa dữ liệu.
- Xóa luôn qua trang xác nhận và vẫn là soft-delete.
- Báo cáo mặc định tháng hiện tại, có comparison kỳ trước, category breakdown và daily trend theo ngày Việt Nam.
- Giao diện SSR hoạt động khi JavaScript bị tắt và responsive ở mobile.
- Startup/shutdown của HTTP server đi cùng bot; địa chỉ bind có default an toàn.

## Out of Scope

- Multi-user, account, password, role hoặc household sharing.
- Public Internet exposure trực tiếp từ app.
- Edit, restore UI, trash, budget, recurring transactions, import hoặc multi-currency.
- SPA, websocket, HTMX, client-side state hoặc public API.
- Forecasting, anomaly detection hoặc financial advice.
- Tự động cài đặt Tailscale, reverse proxy, DNS hoặc TLS.
