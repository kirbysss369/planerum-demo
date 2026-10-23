import {node} from './views.js';
const $ = id => document.getElementById(id);
let currentProgram = null;
let renderedRevision = null;

function populateBinding() {
  const outcome = currentProgram?.outcomes?.find(item => item.id === $('standard-outcome').value);
  const source = currentProgram?.sources?.find(item => item.id === outcome?.source_id);
  $('standard-code').value = outcome?.standard_code || '';
  $('standard-title').value = source?.title || '';
  $('standard-location').value = source?.location || '';
  $('standard-evidence').value = source?.evidence || '';
  $('standard-confirmed').checked = false;
}

export function renderStandardForm(program) {
  const changed = currentProgram?.id !== program?.id || renderedRevision !== program?.revision;
  currentProgram = program;
  renderedRevision = program?.revision;
  const panel = $('standard-panel');
  panel.hidden = !program || program.state === 'draft';
  if (panel.hidden) return;
  const selected = $('standard-outcome').value;
  $('standard-outcome').replaceChildren();
  for (const outcome of program.outcomes || []) {
    const option = node('option', outcome.text);
    option.value = outcome.id;
    $('standard-outcome').appendChild(option);
  }
  if ((program.outcomes || []).some(outcome => outcome.id === selected)) $('standard-outcome').value = selected;
  if (changed) populateBinding();
  $('standard-confirmed').checked = false;
  $('standard-outcome').onchange = () => {populateBinding(); $('standard-confirmed').onchange?.();};
}

export function readStandardBinding() {
  return {
    outcome_id: $('standard-outcome').value,
    code: $('standard-code').value.trim(),
    title: $('standard-title').value.trim(),
    location: $('standard-location').value.trim(),
    evidence: $('standard-evidence').value.trim(),
    confirmed: $('standard-confirmed').checked
  };
}

export function renderRegulatory(report) {
  const panel = $('regulatory');
  panel.replaceChildren();
  panel.hidden = !report;
  if (!report) return;
  panel.appendChild(node('strong', 'Нормативные основания'));
  for (const document of report.basis || []) {
    const paragraph = node('p');
    const link = node('a', document.title);
    link.href = document.location;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    paragraph.appendChild(link);
    paragraph.appendChild(node('span', ' · ' + document.scope));
    panel.appendChild(paragraph);
  }
  if (report.standard_required) panel.appendChild(node('p', 'Подтверждённые привязки к трудовым функциям: ' + report.confirmed_outcomes + '.'));
  panel.appendChild(node('p', report.note, 'muted'));
}
