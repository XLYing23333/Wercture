export const selectionLimit = 100;

export function selectPicture(selected: number[], id: number, checked: boolean): number[] {
  if (!checked) return selected.filter((item) => item !== id);
  if (selected.includes(id) || selected.length >= selectionLimit) return selected;
  return [...selected, id];
}
