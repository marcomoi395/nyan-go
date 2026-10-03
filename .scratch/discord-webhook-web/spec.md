Status: ready-for-agent

# Web gửi tin nhắn và file qua Discord webhook

## Problem Statement

Người dùng cần một trang web đơn giản để gửi tin nhắn hoặc file vào kênh Discord đã cấu hình bằng webhook. Trang web phải yêu cầu mật khẩu, có giao diện 8bit chủ đề Nyan Cat và chạy được trong Docker của dự án.

Repo hiện có bot Discord viết bằng Go, lưu dữ liệu thu chi trong SQLite và được đóng gói bằng Docker Compose. Chưa có giao diện web gửi webhook độc lập. Tính năng mới cần sử dụng được mà không phải cấu hình bot, AI provider hay cơ sở dữ liệu.

## Solution

Thêm một web service Go trong thư mục tính năng riêng, dùng module Go hiện có. Trang web tiếng Việt bắt đầu với màn hình đăng nhập; chỉ hiện biểu mẫu post sau khi server xác thực mật khẩu. Biểu mẫu có ô nội dung, chọn file và nút gửi; hiển thị trạng thái đang gửi, thành công hoặc lỗi.

Backend kiểm tra mật khẩu từ biến môi trường trước khi cho phép gửi, giữ URL webhook ở phía server và chuyển nội dung/file sang Discord. Người dùng có thể gửi chỉ chữ, chỉ file hoặc cả hai. Giới hạn nội dung là 2.000 ký tự; giới hạn file mặc định là 20 MiB theo tài liệu Discord đã đối chiếu.

Giao diện dùng HTML/CSS đơn giản, pixel art Nyan Cat và cầu vồng. Docker Compose chạy web dưới dạng service riêng với image của dự án.

## User Stories

1. As a user, I want to open a simple web page, so that I can send content to Discord without opening the Discord client.
2. As a user, I want to enter the shared password, so that only authorized people can use the sender.
3. As a user, I want the password input to mask my password, so that nearby people cannot read it.
4. As a user, I want an incorrect or missing password to block sending, so that unauthorized requests cannot publish to Discord.
5. As a user, I want to type a message, so that I can publish text to the configured channel.
6. As a user, I want Vietnamese text and emoji preserved, so that the delivered message matches my input.
7. As a user, I want to see the message length limit, so that I know how much text I can send.
8. As a user, I want messages over 2,000 characters rejected clearly, so that I can shorten them before sending.
9. As a user, I want to select a file from my device, so that I can share an attachment.
10. As a user, I want to see the selected file name and size, so that I can check what will be uploaded.
11. As a user, I want to remove or replace the selected file before sending, so that I can correct my selection.
12. As a user, I want to send a file without a message, so that attachments do not require unnecessary text.
13. As a user, I want to send text without a file, so that ordinary messages remain quick to submit.
14. As a user, I want to send text and a file together, so that the attachment has context in the same Discord message.
15. As a user, I want the free-account file limit displayed, so that I can choose a file Discord accepts.
16. As a user, I want oversized files rejected before upload when possible, so that I avoid waiting for a request that cannot succeed.
17. As a user, I want a server-side file limit, so that bypassing browser validation cannot send oversized files.
18. As a user, I want empty submissions rejected, so that I do not accidentally send blank messages.
19. As a user, I want the send button disabled while a request is in progress, so that repeated clicks do not submit the same request concurrently.
20. As a user, I want a success result only after Discord confirms delivery, so that I can trust the result.
21. As a user, I want a clear error when Discord rejects a request, so that I know the message was not confirmed.
22. As a user, I want my input retained after an error, so that I can correct or retry it without retyping.
23. As a user, I want a warning when delivery is uncertain after a timeout, so that I can check Discord before retrying.
24. As a user, I want a useful message when Discord rate-limits sending, so that I know to wait before trying again.
25. As a user, I want mentions disabled by default, so that pasted content does not unexpectedly notify everyone.
26. As a user, I want a simple 8bit Nyan Cat design, so that the page matches the requested theme.
27. As a user, I want the page to work on my phone, so that I can send messages and files away from my computer.
28. As a keyboard user, I want labeled controls and visible focus, so that I can operate the page without a mouse.
29. As a screen-reader user, I want accessible status messages, so that I can tell whether sending succeeded.
30. As an operator, I want the password loaded from an environment variable, so that it is not embedded in source code.
31. As an operator, I want the webhook URL loaded from an environment variable, so that the webhook credential stays on the server.
32. As an operator, I want missing or invalid required configuration to stop startup, so that the service cannot accidentally run without protection.
33. As an operator, I want credentials excluded from browser assets, logs and tracked configuration, so that deployment does not disclose them.
34. As an operator, I want the web service to run through Docker Compose, so that I can deploy it with the existing project.
35. As an operator, I want the web service to start without the expense bot or AI provider, so that webhook sending has no unrelated dependencies.
36. As an operator, I want bounded uploads, timeouts and failed-password attempts, so that a bad request cannot exhaust the service.
37. As an operator, I want temporary uploads cleaned up after every request, so that sent or rejected files do not accumulate.
38. As a maintainer, I want deterministic tests without the real webhook, so that validation never publishes test content to the user's Discord channel.
39. As the project owner, I want implementation and live webhook tests to wait for my explicit instruction, so that creating this spec does not authorize execution.

## Implementation Decisions

- **Tổ chức:** Một thư mục tính năng riêng tên `discord-webhook`, dùng module Go hiện có. Web có executable riêng; không ghép vào vòng lặp bot thu chi và không thêm database.
- **Công nghệ:** Ưu tiên thư viện chuẩn Go cho HTTP, multipart, JSON, biến môi trường và phục vụ giao diện. Không thêm framework frontend/backend hoặc dependency chỉ để gửi webhook.
- **Cấu hình bắt buộc:** `DISCORD_WEB_PASSWORD` và `DISCORD_WEBHOOK_URL`. Thiếu hoặc rỗng thì dừng startup; thông báo lỗi chỉ nêu tên biến, không chứa giá trị bí mật. Chỉ chấp nhận URL HTTPS của Discord với webhook ID/token đúng cấu trúc; không nhận URL đích từ trình duyệt.
- **Giao diện:** Màn hình đăng nhập tiếng Việt trước biểu mẫu post. Nút chọn tệp tùy biến theo theme 8bit thay cho control mặc định; nút bỏ tệp nhỏ nằm bên phải tiêu đề tệp đính kèm. Biểu mẫu gồm nội dung, bộ đếm ký tự, tên/dung lượng file, nút gửi và vùng trạng thái. Pixel art Nyan Cat/cầu vồng dùng tài nguyên local hoặc CSS; không cần CDN, asset sinh bằng AI hoặc hệ thống thiết kế mới.
- **Số lượng file:** Bản đầu hỗ trợ một file mỗi lần gửi để giữ giao diện và upload đơn giản. Đây là giới hạn của ứng dụng, không phải khẳng định giới hạn số attachment của Discord.
- **Hợp đồng HTTP:** `POST /auth` kiểm tra mật khẩu trước khi mở màn hình post, không gọi Discord. Endpoint gửi nhận multipart gồm nội dung và file tùy chọn. Mật khẩu truyền trong header xác thực, giữ trong bộ nhớ trang đến khi tải lại; không đưa mật khẩu vào URL hoặc lưu ở localStorage. Backend xác thực mọi yêu cầu gửi trước khi đọc phần upload lớn hoặc gọi Discord.
- **Xác thực:** So sánh mật khẩu bằng phương thức thời gian hằng. Giới hạn các lần thử sai; không thêm tài khoản, đăng ký, OAuth, JWT hay kho session.
- **Ranh giới trình duyệt:** Không bật CORS tùy ý. Kiểm soát origin và dùng header xác thực không tự được trình duyệt đính kèm để tránh gửi ngoài ý muốn từ website khác. Khi truy cập qua mạng công cộng, lớp triển khai phải cung cấp HTTPS.
- **Nội dung:** Chấp nhận text-only, file-only hoặc text kèm file. Từ chối khi không có file và nội dung chỉ là khoảng trắng. Kiểm tra Unicode hợp lệ, giữ nội dung gốc khi gửi và thống nhất cách đếm 2.000 ký tự giữa frontend/backend, bao gồm tiếng Việt và emoji.
- **Dung lượng:** Giới hạn một file ở 20 MiB, tương đương 20.971.520 byte. Đúng ngưỡng được chấp nhận; vượt một byte bị từ chối. Chặn sớm ở trình duyệt và luôn kiểm tra lại byte thực đọc ở backend; không chỉ tin kích thước do client khai báo.
- **Giới hạn request:** Giới hạn body multipart ở 20 MiB cộng 64 KiB cho nội dung/header multipart. Giới hạn thời gian đọc request, gọi Discord và số upload đồng thời. Không đọc body không giới hạn vào RAM.
- **File:** Giữ byte của file; chỉ chuẩn hóa tên để loại bỏ đường dẫn và ký tự điều khiển. Không tự giải nén, chuyển đổi hay thực thi nội dung. Nếu cần file tạm, dùng vùng tạm được phép ghi và xóa ở cả nhánh thành công/lỗi.
- **Discord:** Dùng Execute Webhook qua HTTPS, gửi nội dung/file bằng multipart với metadata JSON và yêu cầu `wait=true`. Gửi `allowed_mentions` với danh sách parse rỗng. Không chuyển hướng HTTP sang đích khác làm lộ token.
- **Kết quả:** Chỉ báo thành công khi Discord trả xác nhận hợp lệ của message đã tạo. Phân biệt lỗi xác thực, dữ liệu không hợp lệ, file quá lớn, Discord từ chối, rate limit và lỗi mạng. Không trả raw response, token hoặc URL webhook cho trình duyệt.
- **Rate limit và retry:** Tôn trọng thời gian chờ Discord trả về khi bị 429, hiển thị hướng dẫn thử lại. Không tự gửi lại khi timeout hoặc mất kết nối khiến trạng thái giao hàng không chắc chắn; tránh tạo tin nhắn trùng.
- **Trải nghiệm:** Khóa nút gửi trong khi xử lý, thông báo trạng thái dễ đọc và giữ nội dung/file khi lỗi. Không dùng HTML của người dùng để hiển thị nội dung hoặc tên file. Có label, focus rõ, tương phản đủ và trạng thái đọc được bằng screen reader.
- **Docker:** Mở rộng quy trình build image hiện có để chứa executable web. Compose chạy service riêng bằng executable này, truyền riêng hai biến cấu hình và map cổng web; mặc định bind cổng host vào localhost. Service hoạt động độc lập, dùng filesystem chỉ đọc và vùng tạm có giới hạn.
- **Phát hành:** Tận dụng pipeline xuất bản image hiện có. Spec không yêu cầu chạy container, tạo tag, push image hay triển khai lên máy chủ trong lần tạo đặc tả.
- **Quan sát:** Log mã trạng thái/kết quả và thời gian xử lý khi cần, không log mật khẩu, token, URL đầy đủ, nội dung tin nhắn hoặc byte file.

## Testing Decisions

- **Một điểm kiểm thử chính:** Kiểm thử qua HTTP handler của web, quan sát response trả cho người dùng và request gửi ra Discord giả lập. Thay transport của HTTP client bằng transport trong bộ nhớ; không cần thêm interface gửi tin nhắn chỉ phục vụ test.
- **Tiêu chí:** Test hành vi bên ngoài và các ranh giới bảo mật/dung lượng. Không test tên helper, cấu trúc HTML chính xác, chi tiết CSS hoặc từng dòng triển khai.
- **Tiền lệ trong repo:** Các test HTTP provider đã inject HTTP client với transport giả để kiểm tra request, response và lỗi mà không gọi mạng; các test Discord hiện có kiểm tra yêu cầu bị từ chối không chạm backend. Tái sử dụng cách làm này với `testing`, `net/http/httptest` và transport giả.
- **Xác thực:** Thiếu/sai mật khẩu không gọi Discord; mật khẩu đúng cho phép gửi. Không có đường gửi trực tiếp vượt qua xác thực; thử sai nhiều lần bị giới hạn. Request từ origin không được phép bị chặn.
- **Payload:** Kiểm tra text-only, file-only, text kèm file, request rỗng, multipart hỏng và quá nhiều file. Xác nhận nội dung, byte file, tên file an toàn, mentions tắt và chế độ chờ xác nhận.
- **Giới hạn:** Kiểm tra 2.000 ký tự được chấp nhận, 2.001 bị chặn, tiếng Việt và emoji được đếm nhất quán; file đúng 20 MiB được chấp nhận, vượt một byte bị chặn; request vượt body cap không gọi Discord.
- **Lỗi Discord:** Giả lập xác nhận thành công, phản hồi lỗi, webhook hết hiệu lực, 429 kèm thời gian chờ, response hỏng và timeout. Không báo thành công giả hoặc tự retry khi giao hàng chưa rõ.
- **Bí mật và tài nguyên:** Xác nhận response, log, HTML/JS không chứa password/webhook token; body và response bị giới hạn; file tạm được dọn sau lỗi/thành công.
- **Cấu hình:** Bổ sung kiểm tra startup cho hai biến bắt buộc và URL webhook sai, chỉ khi hành vi này chưa được bao phủ ở điểm kiểm thử chính.
- **Giao diện:** Kiểm tra thủ công ở desktop/mobile và bằng bàn phím với Discord giả lập: chọn/bỏ file, bộ đếm, khóa nút, thông báo lỗi/thành công, giữ input và theme Nyan Cat. Không thêm framework screenshot test cho CSS.
- **Docker:** Khi được lệnh triển khai, kiểm tra Compose hợp lệ và build image; khởi chạy riêng service web với cấu hình giả, không cần bot/AI/SQLite. Chạy các kiểm tra Go hiện có sau khi sửa code.
- **Webhook thật:** Không gọi trong test tự động. GET xác minh metadata hoặc gửi thử nội dung/file thật chỉ thực hiện khi người dùng ra lệnh tương ứng.

## Out of Scope

- Triển khai code, chạy Docker hoặc gọi webhook thật trong lần tạo spec này.
- Nhiều file trong một lần gửi; nhiều webhook hoặc chọn kênh đích từ UI.
- Quản lý tài khoản, phân quyền nhiều người, OAuth Discord, JWT hoặc session lưu DB.
- Gửi theo lịch, hàng đợi bền vững, tự retry, lịch sử tin nhắn và dashboard quản trị.
- Chỉnh sửa/xóa tin nhắn đã gửi, embed tùy biến, TTS, poll và interactive components.
- Gửi vào forum/media channel hoặc quản lý thread; bản đầu dành cho webhook của text channel.
- Tăng giới hạn theo Nitro hoặc server boost; nén, chia file hay upload sang dịch vụ lưu trữ khác.
- Thay đổi nghiệp vụ bot thu chi, AI provider, SQLite hoặc reminder.
- Cấp tên miền/chứng chỉ HTTPS, cấu hình reverse proxy hoặc đưa service công khai lên Internet.
- Ghi webhook token/mật khẩu vào spec, source, image hoặc cấu hình được commit.

## Further Notes

- Đặc tả tổng hợp từ trao đổi ngày 03/10/2026. Lệnh dùng skill chỉ cho phép tạo đặc tả; trạng thái `ready-for-agent` không thay thế yêu cầu chờ lệnh triển khai của người dùng.
- Đã đối chiếu [File Attachments FAQ](https://support.discord.com/hc/en-us/articles/25444343291031-File-Attachments-FAQ): tài khoản không Nitro có mức upload 20 MB theo cách diễn đạt của trang hỗ trợ, được tăng từ tháng 08/2026.
- [Discord API — Uploading Files](https://docs.discord.com/developers/reference#uploading-files) xác định giới hạn mặc định theo từng file là 20 MiB. Spec dùng giá trị byte của tài liệu API để triển khai, không dùng mức cũ 10 MB.
- [Webhook Resource — Execute Webhook](https://docs.discord.com/developers/resources/webhook#execute-webhook) xác nhận content tối đa 2.000 ký tự, hỗ trợ file multipart và tùy chọn chờ xác nhận.
- URL người dùng cung cấp đúng dạng webhook nhưng chưa gọi GET xác minh token, tên/kênh hoặc loại kênh, cũng chưa gửi tin nhắn/file thử. Tính hoạt động của token chưa được xác nhận.
- Không sao chép URL chứa token vào issue tracker. Khi triển khai được cho phép, cấu hình webhook qua biến môi trường; người vận hành tự cung cấp mật khẩu.
- Người dùng đã xác nhận điểm kiểm thử HTTP toàn luồng với Discord giả lập. Không bổ sung một tầng kiểm thử client riêng.
