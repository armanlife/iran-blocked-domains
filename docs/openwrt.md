<div dir="rtl">

# تفکیک ترافیک سایت‌های فیلترشده در OpenWrt

برای تفکیک ترافیک داخلی و خارجی در OpenWrt از PassWall 2 استفاده می‌کنیم.
هدف این است که لیست سایت‌های فیلترشده را به روتر بدهیم، تا فقط این سایت‌ها از VPN عبور کنند.

## مرحله یک: دانلود لیست

با SSH به روتر وصل شوید و فایل `ir-blocked.dat` را کنار `geosite.dat` و `geoip.dat` دانلود کنید. این مسیر معمولاً `/usr/share/v2ray` است:

</div>

```bash
wget -O /usr/share/v2ray/ir-blocked.dat https://github.com/armanlife/iran-blocked-domains/releases/latest/download/ir-blocked.dat
```

<div dir="rtl">

برای به‌روزرسانی هفتگی، این خط را در System → Scheduled Tasks اضافه کنید:

</div>

```INI
0 4 * * 1 wget -q -O /usr/share/v2ray/ir-blocked.dat https://github.com/armanlife/iran-blocked-domains/releases/latest/download/ir-blocked.dat
```

<div dir="rtl">

## مرحله دو: ساخت Rule ها

از منوی Services وارد PassWall 2 شوید و در تب Rule Manage دو Rule بسازید.
ترتیب Rule ها مهم است و اگر اشتباه باشد درست کار نمی‌کند.

## مرحله سه: Rule ایران

اولین Rule باید Iran باشد. در این Rule دامنه‌های ir و IPهای ایران را تعریف می‌کنیم:

Domain:

</div>

```INI
regexp:^.+\.ir$
geosite:category-ir
```

<div dir="rtl">

IP:

</div>

```INI
geoip:ir
```

<div dir="rtl">

## مرحله چهار: Rule سایت‌های فیلترشده

دومین Rule را مثلاً Blocked بنامید. در اینجا تعریف می‌کنیم چه دامنه‌ها و IPهایی از VPN عبور کنند.

Domain:

</div>

```INI
ext:ir-blocked.dat:ir-blocked
```

<div dir="rtl">

با این مقدار لیست 268 هزار دامنه را به روتر می‌دهیم. روی روترهای ضعیف ممکن است با کندی و هنگ کردن روتر مواجه شوید.

در قسمت IP می‌توانید رنج IPهای فیلترشده را وارد کنید. پیشنهاد می‌شود IP سرورهای DNS را هم برای عملکرد بهتر وارد کنید:

IP:

</div>

```INI
1.1.1.1
8.8.8.8
8.8.4.4
geoip:telegram
geoip:twitter
```

<div dir="rtl">

## مرحله پنج: مسیریابی

در انتها مسیریابی را طوری تنظیم کنید که Rule ایران مستقیم و Rule سایت‌های فیلترشده از VPN عبور کند.
ترتیب Rule ها بسیار مهم است و اگر اشتباه باشد نتیجه‌ی خوبی نمی‌گیرید.

</div>
