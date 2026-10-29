<div dir="rtl">

# Checker

ابزاری برای بررسی فیلتر بودن دامنه‌ها در ایران. دامنه‌ها به ترتیب رتبه، به صورت موازی و با سرعت قابل تنظیم بررسی می‌شوند.

این ابزار باید **داخل ایران و بدون VPN** اجرا شود.

## نحوه‌ی تشخیص

هر دامنه در دو مرحله بررسی می‌شود:

1. **DNS:** دامنه مستقیماً از DNS سرور (پیش‌فرض 1.1.1.1) پرسیده می‌شود. اگر جواب یکی از IPهای `10.10.34.34`، `10.10.34.35` یا `10.10.34.36` باشد، دامنه فیلتر است (`dns`).
   اگر درخواست UDP بی‌جواب بماند، همان درخواست با TCP تکرار می‌شود. اگر هر دو بی‌جواب بمانند، یک دامنه‌ی آزاد (`example.com`) هم پرسیده می‌شود:
   - اگر جواب بدهد، فقط همین دامنه بی‌جواب است: blackhole شده یا از دسترس خارج است (`dns-timeout`).
   - اگر جواب ندهد، **اینترنت قطع است**. هیچ نتیجه‌ای ثبت نمی‌شود و بررسی صبر می‌کند تا اینترنت برگردد.
2. **SNI:** با IP واقعی دامنه یک اتصال TLS برقرار می‌شود. اگر این اتصال قطع شود ولی همان سرور با نام یک دامنه‌ی آزاد (`example.com`) جواب بدهد، دامنه فیلتر است (`sni`). دامنه‌هایی مثل `x.com` و `discord.com` به این روش فیلتر می‌شوند.

| وضعیت | دسته | معنی |
|---|---|---|
| `blocked` | `blocked-dns` | فیلتر با DNS |
| | `blocked-sni` | فیلتر با SNI |
| `not-blocked` | `not-blocked` | فیلتر نیست |
| `manual` | `dns-timeout` | با UDP و TCP بی‌جواب ماند: blackhole شده یا دامنه از دسترس خارج است |
| | `dns-not-found` | دامنه وجود ندارد یا IPv4 ندارد |
| | `dns-error` | خطاهای دیگر DNS |
| | `connect-failed` | اتصال به IP برقرار نشد: ممکن است IP مسدود باشد یا سرور خاموش باشد |
| | `tls-failed` | TLS با هر نامی شکست خورد: خطای سرور |

## اجرا

</div>

```bash
cd checker
go run ./cmd/checker
```

<div dir="rtl">

یا یک بار build کنید و فایل اجرایی را مستقیم اجرا کنید (در ویندوز با دابل کلیک هم اجرا می‌شود):

</div>

```bash
go build -o checker.exe ./cmd/checker
```

<div dir="rtl">

برای اجرای طولانی در پس‌زمینه روی سرور لینوکس، راهنمای [`deploy`](deploy/README.md) را ببینید.

## پوشه‌ی هر بررسی

هر بررسی یک پوشه در `results` دارد. همه‌ی داده‌ها در یک دیتابیس SQLite (`checker.db`) نگه داشته می‌شوند و لیست‌های متنی از روی آن ساخته می‌شوند:

</div>

```
results/host10m/
├── checker.db           local    ranked list, every result and their history
├── run.json             public   settings, state and count of every category
├── blocked.txt          public   every blocked domain, in rank order
├── blocked-dns.txt      public
├── blocked-sni.txt      public
├── dns-timeout.txt      public
├── dns-not-found.txt    public
├── dns-error.txt        public
├── tls-failed.txt       public
├── connect-failed.txt   local    the second largest list
├── not-blocked.txt      local    the largest list
└── .gitignore           written by checker: keeps the local files out of git
```

<div dir="rtl">

- **همه‌ی لیست‌ها به ترتیب رتبه هستند.** `blocked.txt` از مهم‌ترین دامنه‌ی فیلتر شده شروع می‌شود.
- لیست‌ها در طول بررسی هم به‌روز می‌شوند: هر دقیقه، یا برای لیست‌های بیش از یک میلیون دامنه هر ده دقیقه.
- فایل‌های «local» با `.gitignore` همین پوشه از git بیرون می‌مانند. بقیه را می‌شود با همین ساختار در گیت‌هاب منتشر کرد.
- بررسی هر وقت متوقف شود، دفعه‌ی بعد با دامنه‌هایی که هنوز نتیجه ندارند ادامه پیدا می‌کند.

## لیست رتبه‌دار از چند منبع

`merge` یک لیست رتبه‌دار می‌سازد از یک رتبه‌بندی (مثلاً 10 میلیون دامنه‌ی [host.io](https://host.io/rankings)) و یک لیست قبلی:

- دامنه‌های رتبه‌بندی ترتیب خودشان را دارند.
- دامنه‌ای که فقط در لیست قبلی است، درست بعد از دامنه‌ی اصلی‌اش می‌آید (مثلاً `m.facebook.com` بعد از `facebook.com`). اگر دامنه‌ی اصلی‌اش نبود، بعد از نزدیک‌ترین دامنه‌ی قبل از خودش در لیست قبلی که در رتبه‌بندی هست. مثلاً `ytimg.com` بعد از `googleapis.com` می‌آید، نه ته لیست.

</div>

```bash
checker merge -dir results/host10m -ranking host10m.csv -previous ../data/ir-blocked
checker scan  -dir results/host10m -rate 25
```

<div dir="rtl">

## بررسی دوباره

دامنه‌های یک یا چند دسته را می‌شود دوباره بررسی کرد. هر دامنه رتبه‌ی خودش را نگه می‌دارد و نتیجه‌ی قبلی‌اش در تاریخچه می‌ماند. مثلاً اگر دامنه‌ای از `dns-timeout` به `blocked-dns` برود، در `blocked.txt` سر جای رتبه‌ی خودش قرار می‌گیرد:

</div>

```bash
checker scan -dir results/host10m -recheck dns-timeout,connect-failed
checker find -dir results/host10m youtube.com
```

<div dir="rtl">

بررسی دوباره هم مثل بررسی اصلی قابل توقف و ادامه است. در حالت تعاملی هم با گزینه‌ی «Recheck» انجام می‌شود.

## Release

`data` لیست فیلتر شده را به ترتیب رتبه برای Release آماده می‌کند. چون از `blocked.txt` می‌خواند، بدون دیتابیس هم کار می‌کند:

</div>

```bash
checker data -from results/host10m/blocked.txt -to ../data
```

<div dir="rtl">

## حالت تعاملی

اگر برنامه بدون هیچ پارامتری اجرا شود، قدم به قدم می‌پرسد:

1. بررسی‌های قبلی در `results`: ادامه‌ی بررسی نیمه‌تمام، بررسی دوباره‌ی دسته‌ها، یا شروع بررسی جدید
2. کدام لیست بررسی شود: فایل‌های `.csv` و `.txt` کنار برنامه و فایل‌های پوشه‌ی `data` پروژه
3. سرعت بررسی: 10 تا 200 دامنه در ثانیه، یا عدد دلخواه. روی اینترنت خانگی 25 یا کمتر
4. DNS سرور: Cloudflare، Google، Quad9 یا دلخواه
5. نمایش همه‌ی نتایج یا فقط دامنه‌های فیلتر شده در حین بررسی

بعد از شروع، نتایج به صورت زنده همراه با نوار پیشرفت، سرعت و زمان باقی‌مانده نمایش داده می‌شوند. با `q` بررسی دامنه‌های جدید متوقف می‌شود و بررسی‌های در حال اجرا (حداکثر 20 ثانیه) تمام و ذخیره می‌شوند. با `q` دوم برنامه فوراً بسته می‌شود.

در فایل ورودی هر خط یک دامنه است. فایل‌های CSV مثل `google.com,1` یا `1,google.com` هم پشتیبانی می‌شوند و اولین ستونی که دامنه‌ی معتبر باشد خوانده می‌شود. خط‌های خالی، کامنت‌ها (`#`)، خط‌های بدون دامنه‌ی معتبر و دامنه‌های تکراری نادیده گرفته می‌شوند.

## حالت خط فرمان

</div>

```bash
checker x.com facebook.com                    # quick check, results printed
checker -i domains.txt -format csv -o out.csv # quick check of a list, no run folder
checker scan   -dir results/mylist -i domains.txt -rate 50
checker scan   -dir results/mylist -recheck dns-timeout
checker merge  -dir results/host10m -ranking host10m.csv -previous ../data/ir-blocked
checker export -dir results/host10m           # write the text lists again
checker data   -from results/host10m/blocked.txt -to ../data
checker find   -dir results/host10m youtube.com x.com
```

<div dir="rtl">

با `checker <دستور> -h` فلگ‌های هر دستور نمایش داده می‌شوند. فلگ‌های مشترک:

| فلگ | پیش‌فرض | توضیح |
|---|---|---|
| `-rate` | `10` | حداکثر تعداد دامنه‌ای که در هر ثانیه بررسی آن شروع می‌شود (`0` = بدون محدودیت) |
| `-workers` | بر اساس `-rate` | حداکثر تعداد بررسی همزمان |
| `-dns` | `1.1.1.1` | DNS سرور. هر پوشه تنظیمات اولین اجرایش را نگه می‌دارد |
| `-control` | `example.com` | دامنه‌ی آزاد برای تأیید فیلتر SNI و تشخیص قطع اینترنت |
| `-timeout` | `5s` | مهلت هر درخواست DNS و هر اتصال TLS |
| `-conntrack-limit` | `50` | اگر جدول conntrack لینوکس از این درصد پرتر شود، بررسی دامنه‌های جدید صبر می‌کند (`0` = خاموش) |

با `Ctrl+C` بررسی‌های در حال اجرا تمام و ذخیره می‌شوند و با `Ctrl+C` دوم برنامه فوراً بسته می‌شود.

از DNS مخابرات (`217.218.155.155`) استفاده نکنید. در تست‌ها، برای دامنه‌های فیلتر شده در 40 تا 100 درصد موارد IP واقعی برگرداند و حدود 5٪ درخواست‌ها بی‌جواب ماندند.

## ساختار پروژه

</div>

```
checker/
├── cmd/checker/      entry point: interactive mode without arguments, commands otherwise
├── deploy/           Linux server package: build scripts and systemd service
└── internal/
    ├── check/        DNS and SNI detection of a single domain
    ├── runner/       concurrent checks with rate limit, waiting during network outages
    ├── guard/        pause while the conntrack table is too full
    ├── store/        SQLite database of a run folder, text lists and release files
    ├── session/      a check of a run folder, shared by the command line and the interactive mode
    ├── merge/        ranked list from a ranking and a previous list
    ├── domain/       reading and normalizing domain lists
    ├── report/       text, CSV, JSON and list output of quick checks
    ├── cli/          command line
    └── tui/          interactive questions and live progress
```

<div dir="rtl">

## تست

</div>

```bash
go test ./...
```
