const token = () => document.getElementById('token').value;

export async function call(action, extra = {}) {
  const response = await fetch('/v1/agent', {
    method: 'POST',
    headers: {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token()},
    body: JSON.stringify({action, ...extra})
  });
  const data = await response.json();
  if (!response.ok) {
    const error = new Error(data.error || 'Ошибка запроса');
    error.data = data;
    throw error;
  }
  return data;
}

export async function uploadReference(file) {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch('/v1/references', {
    method: 'POST',
    headers: {'Authorization': 'Bearer ' + token()},
    body: form
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || 'Ошибка чтения файла');
  return data;
}

export async function downloadArtifact(url, name) {
  const response = await fetch(url, {headers: {'Authorization': 'Bearer ' + token()}});
  if (!response.ok) throw new Error('Не удалось скачать ' + name);
  const blob = await response.blob();
  const anchor = document.createElement('a');
  anchor.href = URL.createObjectURL(blob);
  anchor.download = name;
  anchor.click();
  setTimeout(() => URL.revokeObjectURL(anchor.href), 1000);
}
