import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { extraClients } from "../content/clients";
import { ClientChooser } from "./ClientChooser";

it("搜索全部客户端，用键盘选择并关闭目录", async () => {
  const select = vi.fn();
  const user = userEvent.setup();
  render(<ClientChooser current={null} disabled={false} onSelect={select} />);
  await user.click(
    screen.getByRole("button", { name: `全部客户端 · ${extraClients.length}` }),
  );
  expect(screen.getByRole("status")).toHaveTextContent(
    `${extraClients.length} 个客户端`,
  );
  const search = screen.getByRole("searchbox", { name: "搜索客户端" });
  await user.type(search, "hanako");
  expect(screen.getByRole("status")).toHaveTextContent("1 个客户端");
  await user.tab();
  expect(screen.getByRole("button", { name: "OpenHanako" })).toHaveFocus();
  await user.keyboard("{Enter}");
  expect(select).toHaveBeenCalledWith("hanako");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
