import base64, json, pathlib

# Сборка лендинга: python3 landing/build.py
# Правим только landing/src/, результат появляется в landing/dist/ — эту папку и заливаем на хостинг.
import shutil

ROOT = pathlib.Path(__file__).resolve().parent
SRC = ROOT / "src"
FONTS = ROOT / "fonts"
ASSETS = ROOT / "assets"
OUT = ROOT / "dist"
OUT.mkdir(parents=True, exist_ok=True)


def b64(p):
    return base64.b64encode(p.read_bytes()).decode()


font_css = (
    '@font-face{font-family:"Geologica";src:url(data:font/woff2;base64,%s) format("woff2");'
    'font-weight:300 800;font-style:normal;font-display:swap}\n'
    '@font-face{font-family:"JetBrains Mono";src:url(data:font/woff2;base64,%s) format("woff2");'
    'font-weight:400 700;font-style:normal;font-display:swap}\n'
) % (b64(FONTS / "geologica.woff2"), b64(FONTS / "jetbrainsmono.woff2"))

style = font_css + (SRC / "style.css").read_text(encoding="utf-8")

# Language: explicit ?lang= > saved choice > browser languages (ru first wins) > English.
# Runs in <head>, before first paint, so the page never flashes the wrong language.
HEAD_JS = (
    '(function(){var d=document.documentElement,l=null;'
    'try{var q=new URLSearchParams(location.search).get("lang");if(q==="ru"||q==="en"){l=q;try{localStorage.setItem("fr-lang",q)}catch(e){}}}catch(e){}'
    'if(!l){try{var s=localStorage.getItem("fr-lang");if(s==="ru"||s==="en")l=s}catch(e){}}'
    'if(!l){l="en";var a=navigator.languages&&navigator.languages.length?navigator.languages:[navigator.language||""];'
    'for(var i=0;i<a.length;i++){var c=String(a[i]).toLowerCase();if(c.indexOf("ru")===0){l="ru";break}if(c.indexOf("en")===0)break}}'
    'd.setAttribute("lang",l);d.setAttribute("data-lang",l)})();'
)

FOOT_JS_TPL = (
    '(function(){var d=document.documentElement,M=%s;'
    'function set(l){d.setAttribute("lang",l);d.setAttribute("data-lang",l);document.title=M[l].t;'
    'var m=document.querySelector(\'meta[name="description"]\');if(m)m.setAttribute("content",M[l].d);'
    'var b=document.querySelectorAll("[data-set-lang]");for(var i=0;i<b.length;i++){b[i].setAttribute("aria-pressed",b[i].getAttribute("data-set-lang")===l?"true":"false")}}'
    'set(d.getAttribute("data-lang")==="en"?"en":"ru");'
    'var b=document.querySelectorAll("[data-set-lang]");for(var i=0;i<b.length;i++){b[i].addEventListener("click",function(){'
    'var l=this.getAttribute("data-set-lang");try{localStorage.setItem("fr-lang",l)}catch(e){}set(l)})}})();'
)

META = {
    "index.html": {
        "ru": {"t": "Fragmentor — ИИ-тренер по CS2 в Telegram",
               "d": "Fragmentor разбирает демки твоих матчей на FACEIT и объясняет в Telegram, где ты теряешь раунды и что тренировать на этой неделе."},
        "en": {"t": "Fragmentor — AI coach for CS2 in Telegram",
               "d": "Fragmentor reads the demos of your FACEIT matches and explains in Telegram where you lose rounds and what to practice this week."},
    },
    "privacy.html": {
        "ru": {"t": "Политика конфиденциальности — Fragmentor",
               "d": "Какие данные собирает Fragmentor, зачем они нужны и как их удалить."},
        "en": {"t": "Privacy policy — Fragmentor",
               "d": "What data Fragmentor collects, why it needs it and how to have it deleted."},
    },
}

MARK = ('<svg class="mark" viewBox="0 0 32 32" aria-hidden="true">'
        '<path d="M16 2.5v7M16 22.5v7M2.5 16h7M22.5 16h7" fill="none" stroke="currentColor" stroke-width="3.2" stroke-linecap="square"/>'
        '<circle class="mark-dot" cx="16" cy="16" r="4.6"/></svg>')
TG_ICON = ('<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" '
           'd="M2.5 11.2 21.5 3.5l-3.7 16.8-5.6-4.1-2.9 3 .3-4.9 8.4-7.4-10.4 6.5z"/></svg>')
FILE_ICON = ('<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 1.75h4.75l3.5 3.5v9H4zM8.75 1.75v3.5h3.5" '
             'fill="none" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/></svg>')

for name in ("index.html", "privacy.html"):
    html = (SRC / name).read_text(encoding="utf-8")
    html = (html.replace("{{STYLE}}", style)
                .replace("{{HEAD_JS}}", HEAD_JS)
                .replace("{{FOOT_JS}}", FOOT_JS_TPL % json.dumps(META[name], ensure_ascii=False))
                .replace("{{MARK}}", MARK)
                .replace("{{TG_ICON}}", TG_ICON)
                .replace("{{FILE_ICON}}", FILE_ICON))
    assert "{{" not in html, name
    (OUT / name).write_text(html, encoding="utf-8")
    print(name, round(len(html.encode()) / 1024), "KB")

for f in ASSETS.iterdir():
    if f.is_file():
        shutil.copy2(f, OUT / f.name)
print("done ->", OUT)
