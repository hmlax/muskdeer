import type { ReactNode } from 'react';

export interface SelectionNode {
  key?: string | number;
  parentId?: number;
  value?: string | number;
  id?: number;
  title: ReactNode;
  label?: ReactNode;
  children?: SelectionNode[];
  disabled?: boolean;
  selectable?: boolean;
  checkable?: boolean;
}
