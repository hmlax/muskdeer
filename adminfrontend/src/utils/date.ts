export function formatDate(dateString: string | undefined) {
  if (!dateString) return '';
  return new Date(dateString).toLocaleString();
}

// 格式化日期时间
export const formatDateTime = (timestamp?: unknown) => {
  if (!timestamp) return '-';
  try {
    let date: Date;
    if (
      typeof timestamp === 'object' &&
      'seconds' in timestamp &&
      (typeof timestamp.seconds === 'number' || typeof timestamp.seconds === 'string')
    ) {
      date = new Date(Number(timestamp.seconds) * 1000);
    } else if (typeof timestamp === 'string') {
      date = new Date(timestamp);
    } else {
      return '-';
    }
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    // 纯本地格式化：非法日期回退到占位符。
    return '-';
  }
};

// 日期格式化
export const DATE_FORMAT = 'YYYY-MM-DD';
export const TIME_FORMAT = 'YYYY-MM-DD HH:mm:ss';
export const TIME_PICKER_FORMAT = 'HH:mm:ss';
