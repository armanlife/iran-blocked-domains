<div dir=ltr>

# List of blocked domains in Iran
Many domains have been blocked by the government in Iran. 
The purpose of this project is to identify these domains and optimize routing for the traffic of these blocked domains to pass through VPN or Proxy.
This makes it harder to identify the VPN Server for blocking
  
 # Statistics
  The number of domains we have detected are blocked = > 156,000 (one hundred fifty-six thousand)
</div>
 <br>
<div dir=rtl>

# لیست دامنه های فیلتر شده در ایران

# هدف پروژه
هدف این پروژه شناسایی وب سایت های فیلترشده در ایران میباشد و از طریق شناسایی این وب سایت ها میتوانیم فقط ترافیک سایت های فیلتر شده را از روی VPN عبور دهیم
با اینکار میتوانیم شناسایی شدن VPN رو سخت تر کنیم و در مصرف پهنای باند صرفه جویی کنیم
  
  # گزارش اسکن
  
- تعداد دامنه های فیلتر شناسایی شده => 156,000 (صد و پنجاه و شش هزار)


## نحوه تشخیص فیلتر بودن دامنه

یکی از روش های تشخیص فیلتر بودن یک دامنه استفاده از DNS میباشد
اولین قدم برای دسترسی به اینترنت DNS است. وقتی شما آدرس نام دامنه را وارد می کنید، این نام باید به IP تبدیل شود تا دستگاه شما بتواند با سرور آن ارتباط برقرار کند.
 سیستم فیلترینگ ایران با دستکاری در DNS به جای نمایش IP اصلی به یکی از سه IP زیر اشاره میکنند

```INI
10.10.34.34
10.10.34.35
10.10.34.36
```

جهت کسب اطلاعات بیشتر در خصوص DNS به نوشته موجود در [ویکی سانسور][link-wikicensorship] مراجعه کنید

  [بررسی سانسور اینترنت در ارتباط DNS][link-wikicensorship-dns]
 
# نمونه کد در زبان PHP برای تشخیص فیلتر بودن سایت
```php
function CheckBlocked($domain)
{

    $DNS_A = @dns_get_record($domain, DNS_A);

    if (is_countable($DNS_A) == false) {
        //Need to check manually
        $StatusBlocked = "manual";
    } else if (count($DNS_A) == 1 && str_contains($DNS_A[0]['ip'], "10.10.")) {
        $StatusBlocked = true;
    } else {
        $StatusBlocked = false;
    }

    $result = array(
        "StatusBlocked" => $StatusBlocked,
        "dns" => array(
            "A" => $DNS_A,
        ),
    );

    return $result;
}
```

</div> 



[link-wikicensorship]: https://wikicensorship.github.io/fa/docs/measure-internet-censorship/
[link-wikicensorship-dns]: https://wikicensorship.github.io/fa/docs/measure-internet-censorship/DNS/
