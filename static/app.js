import {renderStandardForm, readStandardBinding, renderRegulatory} from './standards.js';
import {call, uploadReference, downloadArtifact} from './client.js';
import {node, renderHours, renderIssues, renderSteps, renderOverview, renderMetrics} from './views.js';
import {editText, setEditText, renderEditor} from './editor.js';

const $ = id => document.getElementById(id);
const states = {draft: 'Уточнение условий', outline_ready: 'Каркас готов к утверждению', approved: 'Каркас утвержден', ready: 'Комплект готов к проверке методистом'};
const specFields = {title:'title', kind:'kind', requested:'requested', audience:'audience', entry_level:'level', delivery:'delivery', attestation:'attestation', requirements:'requirements'};
const specLabels = {title:'Название', kind:'Тип программы', requested:'Объем', audience:'Аудитория', entry_level:'Входной уровень', delivery:'Форма обучения', attestation:'Итоговая аттестация', requirements:'Требования и референсы'};
let program = null;
let busy = false;

function readSpec() {
  const spec = {};
  for (const [key, id] of Object.entries(specFields)) spec[key] = $(id).value;
  spec.requested = $('requested').value === '' ? 0 : Number($('requested').value);
  spec.independent_internship = $('internship').checked;
  return spec;
}

function loadSpec(spec = {}) {
  for (const [key, id] of Object.entries(specFields)) $(id).value = spec[key] || '';
  $('internship').checked = Boolean(spec.independent_internship);
}

function renderQuestions(questions = []) {
  const box = $('questions');
  box.replaceChildren();
  box.hidden = !questions.length;
  if (box.hidden) return;
  box.append(node('strong', 'Нужны уточнения'), node('p', 'Ответьте в сообщении или дополните параметры слева.'));
  const list = node('ol');
  questions.forEach(question => list.appendChild(node('li', question)));
  box.appendChild(list);
}

function renderDialogue(dialogue = []) {
  const box = $('dialogue');
  box.replaceChildren();
  if (!dialogue.length) return;
  box.appendChild(node('h3', 'Обсуждение'));
  for (const item of dialogue) {
    const entry = node('div', null, 'dialogue-entry ' + (item.role === 'user' ? 'user' : 'assistant'));
    entry.append(node('strong', item.role === 'user' ? 'Вы' : 'Планерум'), node('p', item.content));
    box.appendChild(entry);
  }
}

function renderDraft(p) {
  const overview = $('overview');
  overview.replaceChildren();
  const section = node('section', null, 'block');
  section.appendChild(node('h3', 'Собранные условия'));
  const list = node('dl', null, 'spec-list');
  for (const [key, label] of Object.entries(specLabels)) {
    const value = p.spec?.[key];
    if (value === '' || value === 0 || value == null) continue;
    list.append(node('dt', label), node('dd', key === 'kind' ? (value === 'qualification' ? 'Повышение квалификации' : value === 'general' ? 'Общеразвивающая' : value) : key === 'requested' ? value + ' ч' : value));
  }
  section.appendChild(list.childElementCount ? list : node('p', 'Условия пока не указаны.', 'muted'));
  overview.appendChild(section);
  $('hours').replaceChildren(node('strong', 'Баланс часов'), node('p', 'Появится после подготовки каркаса.', 'muted'));
  if (p.issues?.length) renderIssues(p.issues);
  else $('issues').replaceChildren(node('strong', 'Проверки'), node('p', 'Проверьте собранные условия перед подготовкой каркаса.', 'muted'));
}

function setBusy(value) {
  busy = value;
  const draftOrNew = !program || program.state === 'draft';
  $('send').disabled = busy || !$('message').value.trim();
  $('send').textContent = draftOrNew ? 'Отправить' : 'Адаптировать копию';
  $('message').disabled = busy;
  $('bind-standard').disabled = busy || !program || program.state === 'draft' || !$('standard-confirmed').checked;
  for (const id of ['standard-outcome','standard-code','standard-title','standard-location','standard-evidence','standard-confirmed']) $(id).disabled = busy;
  $('create').disabled = busy || !draftOrNew;
  $('new-request').disabled = busy;
  $('open').disabled = busy;
  $('read-reference').disabled = busy || !draftOrNew;
  for (const id of [...Object.values(specFields), 'internship', 'reference']) $(id).disabled = busy || !draftOrNew;
  $('edit-apply').disabled = busy || !program || program.state === 'draft';
  $('approve').disabled = busy || program?.state !== 'outline_ready';
  $('generate').disabled = busy || program?.state !== 'approved';
  $('export').disabled = busy || program?.state !== 'ready' || program.approved_revision !== program.outline_revision;
}

function setStatus(message, error = false) {
  $('state').textContent = message;
  $('state').className = error ? 'error' : 'muted';
}

function show(data) {
  if (data.program) program = data.program;
  else if (data.questions?.length) program = null;
  renderStandardForm(program);
  renderRegulatory(data.regulatory);
  renderQuestions(program?.questions || data.questions || []);
  renderDialogue(program?.dialogue || []);
  $('files').replaceChildren();
  if (!program) {
    $('canvas-title').textContent = 'Программа еще не создана';
    $('revision').textContent = 'Новый запрос';
    $('overview').replaceChildren(node('div', 'Каркас, результаты обучения и план появятся здесь.', 'empty'));
    $('editor').hidden = true;
    $('raw').hidden = true;
    $('result').textContent = '';
    $('program-id').value = '';
    $('hours').replaceChildren(node('strong', 'Баланс часов'), node('p', 'Появится после подготовки каркаса.', 'muted'));
    $('issues').replaceChildren(node('strong', 'Проверки'), node('p', 'Появятся после подготовки каркаса.', 'muted'));
    $('usage').textContent = 'Стоимость пока не рассчитана.';
    renderSteps(null);
    history.replaceState(null, '', location.pathname);
    setBusy(busy);
    return;
  }
  loadSpec(program.spec);
  $('canvas-title').textContent = program.spec?.title || 'Программа без названия';
  $('revision').textContent = 'Версия ' + program.revision;
  setStatus(states[program.state] || program.state);
  $('program-id').value = program.id;
  $('approve').textContent = 'Утвердить каркас v' + program.outline_revision;
  if (program.state === 'draft') {
    $('parameters-details').open = true;
    renderDraft(program);
    $('editor').hidden = true;
  } else {
    renderOverview(program, data.coverage);
    renderEditor(program);
    renderHours(program);
    renderIssues(data.issues || program.issues || []);
  }
  renderSteps(program.state);
  renderMetrics(program);
  $('result').textContent = JSON.stringify(program, null, 2);
  $('raw').hidden = false;
  history.replaceState(null, '', '?program_id=' + encodeURIComponent(program.id));
  setBusy(busy);
}

async function run(action, extra = {}) {
  if (busy) return;
  setBusy(true);
  setStatus(({adapt:'Адаптируем копию программы', bind_standard:'Сохраняем нормативную привязку', intake:'Сохраняем запрос', answer:'Сохраняем ответ', create:'Подготовка каркаса', approve_outline:'Утверждение', generate:'Подготовка материалов', status:'Загрузка программы'}[action] || action) + '…');
  try {
    show(await call(action, extra));
    if (action === 'intake' || action === 'answer' || action === 'adapt') $('message').value = '';
  } catch (error) {
    if (error.data?.program) show(error.data);
    setStatus(error.message, true);
  } finally {
    $('message-count').textContent = $('message').value.length + ' / 4000';
    setBusy(false);
  }
}

$('message').addEventListener('input', () => {
  $('message-count').textContent = $('message').value.length + ' / 4000';
  setBusy(busy);
});
$('send').onclick = () => {
  const message = $('message').value.trim();
  if (!message || message.length > 4000 || busy) return;
  if (program && program.state !== 'draft') {
    run('adapt', {program_id: program.id, revision: program.revision, message});
    return;
  }
  const action = program ? 'answer' : 'intake';
  run(action, {message, ...(program ? {program_id:program.id, revision:program.revision} : {}), spec:readSpec()});
};
$('create').onclick = () => {
  if (busy || (program && program.state !== 'draft')) return;
  const spec = readSpec();
  run('create', program ? {program_id:program.id, revision:program.revision, spec} : {spec});
};
$('new-request').onclick = () => {
  if (busy) return;
  program = null;
  loadSpec();
  $('message').value = '';
  $('message-count').textContent = '0 / 4000';
  $('reference').value = '';
  $('parameters-details').open = false;
  show({});
  setStatus('Опишите новый курс или заполните параметры.');
  $('message').focus();
};
$('read-reference').onclick = async () => {
  if (busy || (program && program.state !== 'draft')) return;
  setBusy(true);
  try {
    const file = $('reference').files[0];
    if (!file) throw new Error('Выберите файл.');
    const data = await uploadReference(file);
    const addition = ($('requirements').value ? '\n\n' : '') + 'Референс ' + data.name + ' (SHA-256 ' + data.sha256 + '):\n' + data.text;
    if ($('requirements').value.length + addition.length > 64000) throw new Error('Текст файла превышает лимит требований в 64 000 символов.');
    $('requirements').value += addition;
    setStatus('Текст файла добавлен к требованиям.');
  } catch (error) { setStatus(error.message, true); }
  finally { setBusy(false); }
};
$('standard-confirmed').onchange = () => setBusy(busy);
$('bind-standard').onclick = () => {
  if (busy || !program || program.state === 'draft' || !$('standard-confirmed').checked) return;
  run('bind_standard', {program_id: program.id, revision: program.revision, binding: readStandardBinding()});
};
$('approve').onclick = () => program?.state === 'outline_ready' && run('approve_outline', {program_id:program.id, revision:program.revision});
$('generate').onclick = async () => {
  if (!program || program.state !== 'approved' || busy) return;
  setBusy(true);
  try {
    let response = await call('generate_async', {program_id:program.id, revision:program.revision});
    let job = response.job;
    const deadline = Date.now() + 35 * 60 * 1000;
    setStatus('Задание поставлено в очередь · ' + job.id);
    while (job.state === 'queued' || job.state === 'running') {
      if (Date.now() > deadline) throw new Error('Задание ещё выполняется. Откройте программу позже, чтобы проверить результат.');
      await new Promise(resolve => setTimeout(resolve, 1500));
      response = await call('job_status', {job_id:job.id});
      job = response.job;
      setStatus(job.state === 'running' ? 'Подготовка материалов выполняется…' : 'Ожидание свободного исполнителя…');
    }
    show(response);
    if (job.state === 'failed') throw new Error(job.error || 'Генерация не завершилась');
  } catch (error) {
    if (error.data?.program) show(error.data);
    setStatus(error.message, true);
  } finally { setBusy(false); }
};
$('open').onclick = () => {
  const id = $('program-id').value.trim();
  if (!id) {setStatus('Укажите идентификатор программы.', true); return;}
  run('status', {program_id:id});
};
$('edit-target').onchange = () => {if (program) $('edit-value').value = editText(program, $('edit-target').value);};
$('edit-apply').onclick = () => {
  if (!program || program.state === 'draft' || busy) return;
  const proposed = structuredClone(program);
  try {setEditText(proposed, $('edit-target').value, $('edit-value').value.trim());}
  catch (error) {setStatus(error.message, true); return;}
  run('revise', {program_id:program.id, revision:program.revision, program:proposed});
};
$('export').onclick = async () => {
  if (!program || program.state !== 'ready' || busy) return;
  setBusy(true);
  setStatus('Сборка файлов…');
  try {
    const data = await call('export', {program_id:program.id, revision:program.revision});
    const files = $('files');
    files.replaceChildren();
    for (const [name, url] of Object.entries(data.artifacts || {})) {
      const link = node('a', 'Скачать ' + name);
      link.href = '#';
      link.onclick = async event => {
        event.preventDefault();
        try {await downloadArtifact(url, name);}
        catch (error) {setStatus(error.message, true);}
      };
      files.appendChild(link);
    }
    setStatus('Файлы версии ' + program.revision + ' собраны.');
  } catch (error) {
    if (error.data?.program) show(error.data);
    setStatus(error.message, true);
  } finally {setBusy(false);}
};
const initialID = new URLSearchParams(location.search).get('program_id');
if (initialID) $('program-id').value = initialID;
setBusy(false);
