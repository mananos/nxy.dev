// Capturas con Chromium dentro de nxy-docs-video (cwd = raiz del repo).
//   node docs/assets/brand/captura.cjs captura <nombres...>   capturas de la TUI -> docs/assets/capturas/<n>.png (2560 de ancho)
//   node docs/assets/brand/captura.cjs banner                 portada -> docs/assets/brand/banner.png (2560x1440)
//   node docs/assets/brand/captura.cjs diagrama <nombres...>  diagramas -> docs/assets/diagrams/<n>.png (2560x1440)
//   node docs/assets/brand/captura.cjs fragmento <nombre>     imprime el HTML de un volcado (para los videos)
//   node docs/assets/brand/captura.cjs prueba                 prueba ansiAHtml sin Chromium
// Las capturas leen docs/assets/capturas/capturas.tsv y los volcados docs/assets/.build/dumps/<n>.ansi.
// Falla (exit 1) si no cargo una fuente de la marca o si la grilla de la TUI se desalinea: nada de fuente
// de reemplazo en silencio.
const fs = require('fs');
const path = require('path');
const { pathToFileURL } = require('url');

const ASSETS = path.resolve('docs/assets');
const FG = '#DEDBD2';
const BG = '#070707';

function fail(msg) { console.error('captura: ' + msg); process.exit(1); }

// El ✓ y el ✗ no estan en JetBrains Mono y cae a otra fuente mas ancha: va en su propia celda (clase .g de marca.css).
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/[✓✗]/g, (c) => '<span class="g">' + c + '</span>');

// ansiAHtml convierte texto con secuencias SGR en una <div class="fila"> por linea y un <span> por tramo.
// Entiende reset, negrita, italica, subrayado, invertido y color 24 bits de texto y fondo; el resto se ignora.
function ansiAHtml(texto) {
  const lineas = texto.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n');
  let fg = null, bg = null, neg = false, ita = false, sub = false, inv = false;
  const estilo = () => {
    let f = fg, b = bg;
    if (inv) { const x = f || FG; f = b || BG; b = x; }
    const c = [];
    if (f) c.push('color:' + f);
    if (b) c.push('background:' + b);
    if (neg) c.push('font-weight:700');
    if (ita) c.push('font-style:italic');
    if (sub) c.push('text-decoration:underline');
    return c.join(';');
  };
  const sgr = (params) => {
    const p = params === '' ? [0] : params.split(';').map((x) => (x === '' ? 0 : Number(x)));
    for (let i = 0; i < p.length; i++) {
      const v = p[i];
      if (v === 0) { fg = bg = null; neg = ita = sub = inv = false; }
      else if (v === 1) neg = true;
      else if (v === 22) neg = false;
      else if (v === 3) ita = true;
      else if (v === 23) ita = false;
      else if (v === 4) sub = true;
      else if (v === 24) sub = false;
      else if (v === 7) inv = true;
      else if (v === 27) inv = false;
      else if (v === 39) fg = null;
      else if (v === 49) bg = null;
      else if ((v === 38 || v === 48) && p[i + 1] === 2) {
        const c = 'rgb(' + p[i + 2] + ',' + p[i + 3] + ',' + p[i + 4] + ')';
        if (v === 38) fg = c; else bg = c;
        i += 4;
      } else if ((v === 38 || v === 48) && p[i + 1] === 5) i += 2;
    }
  };
  const out = [];
  for (const linea of lineas) {
    let html = '';
    let pos = 0;
    const re = /\x1b\[([0-9;?]*)([A-Za-z])/g;
    let m;
    const tramo = (t) => {
      if (t === '') return;
      const st = estilo();
      html += st ? '<span style="' + st + '">' + esc(t) + '</span>' : esc(t);
    };
    while ((m = re.exec(linea)) !== null) {
      tramo(linea.slice(pos, m.index));
      pos = m.index + m[0].length;
      if (m[2] === 'm') sgr(m[1]);
    }
    tramo(linea.slice(pos));
    out.push('<div class="fila">' + html + '</div>');
  }
  return out.join('\n');
}

function prueba() {
  const casos = [
    ['hola', '<div class="fila">hola</div>'],
    ['\x1b[38;2;253;191;1mA\x1b[0m<b&', '<div class="fila"><span style="color:rgb(253,191,1)">A</span>&lt;b&amp;</div>'],
    ['\x1b[1;48;2;1;2;3mX\x1b[22mY\x1b[m', '<div class="fila"><span style="background:rgb(1,2,3);font-weight:700">X</span><span style="background:rgb(1,2,3)">Y</span></div>'],
    ['\x1b[7mR\x1b[27m\x1b[3;4mI\x1b[23;24m\x1b[39m', '<div class="fila"><span style="color:' + BG + ';background:' + FG + '">R</span><span style="font-style:italic;text-decoration:underline">I</span></div>'],
    ['a\x1b[2Kb\nc\n', '<div class="fila">ab</div>\n<div class="fila">c</div>'],
    ['ok ✓ no ✗', '<div class="fila">ok <span class="g">✓</span> no <span class="g">✗</span></div>'],
    ['\x1b[38;5;9mq\x1b[1mz', '<div class="fila">q<span style="font-weight:700">z</span></div>'],
  ];
  let mal = 0;
  casos.forEach(([entrada, esperado], i) => {
    const got = ansiAHtml(entrada);
    if (got !== esperado) { mal++; console.error('prueba ' + i + ':\n  got  ' + got + '\n  want ' + esperado); }
  });
  if (mal) process.exit(1);
  console.log('prueba: ' + casos.length + ' casos ok');
}

function leerTsv() {
  const f = path.join(ASSETS, 'capturas', 'capturas.tsv');
  const lineas = fs.readFileSync(f, 'utf8').split(/\r?\n/).filter((l) => l.trim() !== '');
  const cols = lineas[0].split('\t');
  return lineas.slice(1).map((l) => {
    const v = l.split('\t');
    const r = {};
    cols.forEach((c, i) => { r[c] = (v[i] || '').trim(); });
    return r;
  });
}

function volcado(n) {
  const f = path.join(ASSETS, '.build', 'dumps', n + '.ansi');
  if (!fs.existsSync(f)) fail('no existe ' + f + ' (lo genera render.sh capturas)');
  return fs.readFileSync(f, 'utf8');
}

async function listo(page) {
  await page.evaluate(async () => {
    await Promise.all([
      document.fonts.load('900 30px Archivo'),
      document.fonts.load('16px "JetBrains Mono"'),
      document.fonts.load('700 16px "JetBrains Mono"'),
    ]);
    await document.fonts.ready;
    await Promise.all([...document.images].map((i) => i.decode()));
  });
  const ok = await page.evaluate(() => {
    const cargada = (n) => [...document.fonts].some((f) => f.family.replace(/["']/g, '').includes(n) && f.status === 'loaded');
    return cargada('Archivo') && cargada('JetBrains Mono');
  });
  if (!ok) fail('la fuente Archivo o JetBrains Mono no cargo (Google Fonts sin red?)');
}

async function main() {
  const [modo, ...nombres] = process.argv.slice(2);
  if (modo === 'prueba') return prueba();
  if (modo === 'fragmento') {
    if (nombres.length !== 1) fail('uso: captura.cjs fragmento <nombre>');
    return console.log(ansiAHtml(volcado(nombres[0])));
  }
  if (!['captura', 'banner', 'diagrama'].includes(modo)) fail('uso: captura.cjs captura|banner|diagrama|fragmento|prueba [nombres...]');
  if (modo !== 'banner' && nombres.length === 0) fail('uso: captura.cjs ' + modo + ' <nombres...>');
  const puppeteer = require('/opt/video/node_modules/puppeteer-core');
  const browser = await puppeteer.launch({ executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] });
  try {
    if (modo === 'banner') {
      const page = await browser.newPage();
      await page.setViewport({ width: 1280, height: 720, deviceScaleFactor: 2 });
      await page.goto(pathToFileURL(path.join(ASSETS, 'brand', 'banner.html')).href, { waitUntil: 'networkidle0' });
      await listo(page);
      await page.screenshot({ path: path.join(ASSETS, 'brand', 'banner.png') });
      await page.close();
      console.log('captura: banner');
    } else if (modo === 'diagrama') {
      for (const n of nombres) {
        const html = path.join(ASSETS, 'diagrams', n + '.html');
        if (!fs.existsSync(html)) fail('no existe ' + html);
        const page = await browser.newPage();
        await page.setViewport({ width: 1280, height: 720, deviceScaleFactor: 2 });
        await page.goto(pathToFileURL(html).href, { waitUntil: 'networkidle0' });
        await listo(page);
        await page.screenshot({ path: path.join(ASSETS, 'diagrams', n + '.png') });
        await page.close();
        console.log('captura: diagrama ' + n);
      }
    } else {
      const filas = leerTsv();
      for (const n of nombres) {
        const fila = filas.find((r) => r.nombre === n);
        if (!fila) fail(n + ' no esta en capturas.tsv');
        const html = ansiAHtml(volcado(n));
        const page = await browser.newPage();
        await page.setViewport({ width: 1280, height: 1400, deviceScaleFactor: 2 });
        await page.goto(pathToFileURL(path.join(ASSETS, 'brand', 'captura-fondo.html')).href, { waitUntil: 'networkidle0' });
        await page.evaluate((d, term) => {
          document.querySelector('[data-campo=titulo]').textContent = d.titulo;
          document.querySelector('[data-campo=sub]').textContent = d.sub;
          document.querySelector('[data-campo=chip]').textContent = 'nxy tui';
          document.getElementById('term').innerHTML = term;
        }, fila, html);
        await listo(page);
        const r = await page.evaluate(() => {
          const sonda = document.createElement('span');
          sonda.style.cssText = 'position:absolute;visibility:hidden;white-space:pre;font:16px "JetBrains Mono",monospace';
          sonda.textContent = 'M'.repeat(100);
          document.body.appendChild(sonda);
          const adv = sonda.getBoundingClientRect().width / 100;
          sonda.remove();
          const malas = [];
          document.querySelectorAll('#term .fila').forEach((f, i) => {
            const rg = document.createRange();
            rg.selectNodeContents(f);
            const w = rg.getBoundingClientRect().width;
            const celdas = Array.from(f.textContent).length;
            if (Math.abs(w - celdas * adv) > 0.5) malas.push('fila ' + (i + 1) + ': ' + w.toFixed(1) + 'px, esperaba ' + (celdas * adv).toFixed(1) + ' (' + celdas + ' celdas)');
          });
          const alto = document.getElementById('term').getBoundingClientRect().height;
          return { adv, malas, alto };
        });
        if (r.malas.length) fail(n + ': la grilla se desalinea (algun glifo cayo a otra fuente):\n  ' + r.malas.slice(0, 8).join('\n  '));
        const total = Math.ceil(96 + r.alto + 52);
        await page.evaluate((h) => { document.querySelector('.fondo').style.height = h + 'px'; }, total);
        await page.setViewport({ width: 1280, height: total, deviceScaleFactor: 2 });
        const out = path.join(ASSETS, 'capturas', n + '.png');
        fs.mkdirSync(path.dirname(out), { recursive: true });
        await page.screenshot({ path: out, clip: { x: 0, y: 0, width: 1280, height: total } });
        await page.close();
        console.log('captura: ' + n + ' (' + total + ' px de alto)');
      }
    }
  } finally {
    await browser.close();
  }
}

main().catch((e) => fail(e.stack || String(e)));
