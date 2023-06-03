<div dir=rtl>

# تفکیک ترافیک داخلی و بین المللی در OpenWrt


جهت تفکیک ترافیک خارجی و داخلی در openwrt باید از passwall استفاده کنیم
هدف ما این است که لیست سایت های فیلتر شده رو وارد تنظمیات روتر کنیم و به شکلی تنظیم کنیم که فقط سایت های فیلتره شده از روی vpn عبور کنند


## مرحله یک
از منو Services وارد PassWall 2 میشویم
سپس وارد تب Rule Manage میشیم آپدیت خودکار هفتگی رو فعال میکنیم  
و آدرس قسمت Custom geosite URL را به این آدرس زیر تغییر میدهیم:


```INI
https://api.github.com/repos/filteryab/v2ray-rules-dat/releases/latest
```
![[openwrt](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/01.jpg](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/01.jpg)

و روی دکمه Manually update یکبار میزنیم که دیتا آپدیت شود.

## مرحله دو

مورد بعد ساخت یک Rule به اسم دلخواه هست که ما اینجا اسم Filteryab را براش میزاریم
و داخلش باید قوانین مربوطه را تعریف کنیم
نکته مهم ترتیب قرارگیری این Rule ها هست
که اگر اشتباه باشد به درستی کار نخواهد کرد

![[openwrt](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/02.jpg](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/02.jpg)
## مرحله سوم
اولین Rule باید Iran باشد
در این Rule دامنه های ir و ip های ایران را تعریف میکنیم

![[openwrt](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/03.jpg](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/03.jpg)

Domain:
```INI
regexp:^.+\.ir$
geosite:category-ir
```

IP:
```INI
geoip:ir
```

## مرحله چهارم
دومین Rule باید Filteryab باشد
در اینجا تعریف میکنیم چه دامنه های و چه ip های از v2ray عبور کنند
برای تعریف دامنه دو حالت وجود دارد
1️⃣ حالت یک برای روتر های بسیار قوی است که به شکل کلی پیشنهاد نمیشود

![[openwrt](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/04.jpg](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/04.jpg)

Domain:
```INI
geosite:ir-blocked-domain
```
با وارد کردن این مقدار لیست 156 هزار دامنه رو به روتر میدهیم در صورتی که روتر ضعیف باشد با کندی و هنگی روتر مواجه میشود
خود بنده روتر Xiaomi Redmi Router AX6S دارم که وقتی بهش لیست کامل رو دادم خیلی کند شد و بیخیالش شدم
روش دوم خیلی بهتر هست که در پایین به ان اشاره میکنیم


2️⃣روش دوم
در این روش هر 10 هزار دامنه داخل یک دسته بندی قرار گرفته اند و دامنه ها بر اساس رنکینگ مرتب شده اند
میتوانید تا جایی که روتر قادر به پردازش هست دستی بندی بهش اضافه کنید

```INI
geosite : ir-blocked-domain-01
geosite : ir-blocked-domain-02
geosite : ir-blocked-domain-03
geosite : ir-blocked-domain-04
geosite : ir-blocked-domain-05
geosite : ir-blocked-domain-06
geosite : ir-blocked-domain-07
geosite : ir-blocked-domain-08
geosite : ir-blocked-domain-09
geosite : ir-blocked-domain-10
geosite : ir-blocked-domain-11
geosite : ir-blocked-domain-12
geosite : ir-blocked-domain-13
geosite : ir-blocked-domain-14
geosite : ir-blocked-domain-15
```

در قسمت IP میتوانید رنج ip های فیلترشده را وارد کنید و پیشنهاد میشود که ip dns ها جهت عملکرد بهتر وارد کنید

IP:
```INI
1.1.1.1
8.8.8.8
8.8.4.4
geoip:telegram
geoip:twitter
```


![[openwrt](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/05.jpg](https://raw.githubusercontent.com/filteryab/filteryab.github.io/hidden/assets/image/openwrt/05.jpg)

و در انتها روتینگ را به این شکل تنظیم میکنیم

در واقع به روتر میگویم دامنه ها و ip های داخل Filteryab را از روی v2ray عبور دهد

بسیار مهم هست که ترتیب Rule ها درست باشد در صورت اشتباه بودن ترتیب Rule ها نتجیه خوبی نخواهید گرفت
</div> 


