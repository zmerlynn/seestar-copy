// The browse page: lists one folder of the share at a time. Every name is set
// with textContent and every URL built with encodeURIComponent.
const $ = (id) => document.getElementById(id);
const statusLine = $('status');
const table = $('list');
const rows = table.tBodies[0];
const crumbs = $('crumbs');
const viewer = $('viewer');
const viewed = viewer.querySelector('img');

let generation = 0;
let loading = null;

const pageURL = (p) => (p ? '/?path=' + encodeURIComponent(p) : '/');
const fileURL = (p, save) => '/file?path=' + encodeURIComponent(p) + (save ? '&download=1' : '');
const baseName = (p) => p.slice(p.lastIndexOf('/') + 1);
const plainClick = (ev) => ev.button === 0 && !ev.ctrlKey && !ev.metaKey && !ev.shiftKey && !ev.altKey;

function el(tag, text, cls) {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
}

function humanSize(n) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000;
    i++;
  }
  return (i ? v.toFixed(1) : String(v)) + ' ' + units[i];
}

function folderLink(p, text) {
  const a = el('a', text);
  a.href = pageURL(p);
  a.addEventListener('click', (ev) => {
    if (!plainClick(ev)) return;
    ev.preventDefault();
    history.pushState(null, '', pageURL(p));
    load(p);
  });
  return a;
}

function showCrumbs(p) {
  crumbs.replaceChildren(folderLink('', 'Share'));
  if (!p) return;
  const parts = p.split('/');
  parts.forEach((part, i) => crumbs.append(' / ', folderLink(parts.slice(0, i + 1).join('/'), part)));
}

function view(p, name) {
  viewed.src = fileURL(p);
  $('vname').textContent = name;
  $('vsave').href = fileURL(p, true);
  viewer.showModal();
}

function row(e) {
  const name = baseName(e.path);
  const file = e.type === 'file';
  const thumb = el('td', undefined, 'thumb');
  if (e.thumb) {
    const img = el('img');
    img.alt = '';
    img.loading = 'lazy';
    img.decoding = 'async';
    img.addEventListener('error', () => { img.hidden = true; });
    img.src = fileURL(e.thumb);
    thumb.append(img);
  }
  const nameCell = el('td');
  if (!file) {
    nameCell.append(folderLink(e.path, name + '/'));
  } else if (e.view) {
    const a = el('a', name);
    a.href = fileURL(e.path);
    a.addEventListener('click', (ev) => {
      if (!plainClick(ev)) return;
      ev.preventDefault();
      view(e.path, name);
    });
    nameCell.append(a);
  } else {
    nameCell.textContent = name;
  }
  const size = el('td', file ? humanSize(e.size) : '', 'num');
  if (file) size.title = e.size + ' bytes';
  const modified = el('td', new Date(e.mtime).toLocaleString());
  modified.title = e.mtime;
  const status = el('td', undefined, 'st');
  if (e.status) status.append(el('span', e.status, 'badge ' + e.status));
  const save = el('td');
  if (file) {
    const a = el('a', 'Save');
    a.href = fileURL(e.path, true);
    a.download = '';
    save.append(a);
  }
  const tr = el('tr');
  tr.append(thumb, nameCell, size, modified, status, save);
  return tr;
}

function render(data) {
  $('endpoint').textContent = data.endpoint;
  table.classList.toggle('statuses', data.statuses);
  const dirs = data.entries.filter((e) => e.type === 'dir');
  const files = data.entries.filter((e) => e.type === 'file');
  rows.replaceChildren(...dirs.map(row), ...files.map(row));
  table.hidden = rows.children.length === 0;
  statusLine.textContent = table.hidden ? 'Empty folder' : '';
}

async function load(p) {
  const mine = ++generation;
  if (loading) loading.abort();
  loading = new AbortController();
  showCrumbs(p);
  statusLine.textContent = 'Loading…';
  table.hidden = true;
  rows.replaceChildren();
  let data;
  try {
    const res = await fetch('/api/list?path=' + encodeURIComponent(p), { signal: loading.signal });
    data = await res.json();
    if (mine !== generation) return;
    if (!res.ok) {
      statusLine.textContent = data.message;
      return;
    }
  } catch (err) {
    if (mine !== generation) return;
    statusLine.textContent = 'Could not load this folder: ' + err.message;
    return;
  }
  render(data);
}

const current = () => new URLSearchParams(location.search).get('path') || '';

$('vclose').addEventListener('click', () => viewer.close());
viewer.addEventListener('close', () => viewed.removeAttribute('src'));
window.addEventListener('popstate', () => load(current()));
load(current());
