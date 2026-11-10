import { data } from "react-router";

export function clientLoader() {
  throw data({ message: "The page you are looking for does not exist." }, { status: 404 });
}

export default function NotFound() {
  return null;
}
