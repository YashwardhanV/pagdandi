import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { useApp } from "../../state/store";
import { routeResponse, scOption, stdOption } from "../../test/fixtures";
import { RouteCompareCard, RouteCompareSkeleton } from "./RouteCompareCard";
import { ConfidenceChip, SurfaceBar, WarningChips, bandOf } from "./parts";

beforeEach(() => useApp.setState({ lang: "en" }));

describe("RouteCompareCard", () => {
  it("shows the usual way and the pagdandi with savings and the recommended tag", () => {
    render(<RouteCompareCard options={routeResponse.options} recommended="sc1" selected="sc1" onSelect={() => {}} country="IN" />);
    const std = screen.getByTestId("row-std");
    const sc = screen.getByTestId("row-sc1");
    expect(within(std).getByText("Usual way")).toBeInTheDocument();
    expect(within(std).getByText("17 min")).toBeInTheDocument();
    expect(within(std).getByText("7.1 km")).toBeInTheDocument();
    expect(within(sc).getByText("Pagdandi")).toBeInTheDocument();
    expect(within(sc).getByText("Recommended")).toBeInTheDocument();
    expect(within(sc).getByText("saves 7 min")).toBeInTheDocument();
    expect(within(sc).getByText(/Kaccha road · 1.2 km/)).toBeInTheDocument();
    expect(within(sc).getByText("Gate")).toBeInTheDocument();
    expect(within(sc).getByText(/Confirmed 3 days ago by 4 riders/)).toBeInTheDocument();
    expect(within(std).queryByText("Recommended")).not.toBeInTheDocument();
  });

  it("marks the selected row and selects by click and keyboard", async () => {
    const onSelect = vi.fn();
    render(<RouteCompareCard options={routeResponse.options} recommended="sc1" selected="sc1" onSelect={onSelect} />);
    expect(screen.getByTestId("option-sc1")).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("row-sc1").className).toContain("border-accent");
    expect(screen.getByTestId("option-std")).toHaveAttribute("aria-checked", "false");
    await userEvent.click(screen.getByTestId("option-std"));
    expect(onSelect).toHaveBeenCalledWith("std");
    screen.getByTestId("option-sc1").focus();
    await userEvent.keyboard("{Enter}");
    expect(screen.getByTestId("option-sc1")).toHaveAccessibleName(/Pagdandi.*Recommended.*10 min/);
    expect(onSelect).toHaveBeenCalledWith("sc1");
  });

  it("uses the Swedish local word for unpaved roads", () => {
    useApp.setState({ lang: "sv" });
    render(<WarningChips warnings={scOption.warnings} country="SE" />);
    expect(screen.getByText(/Grusväg/)).toBeInTheDocument();
  });

  it("labels an OSM-only shortcut honestly", () => {
    const osmOnly = { ...scOption, legs: [{ source: "osm" as const, from_idx: 0, to_idx: 7 }], confidence: undefined };
    render(<RouteCompareCard options={[stdOption, osmOnly]} recommended="std" selected="std" onSelect={() => {}} />);
    expect(screen.getByText("Mapped road · no rider reports yet")).toBeInTheDocument();
  });

  it("reserves height while loading", () => {
    const { container } = render(<RouteCompareSkeleton />);
    expect(container.querySelectorAll(".h-\\[132px\\]")).toHaveLength(2);
  });
});

describe("SurfaceBar", () => {
  it("sizes segments by metres and reveals a legend on tap", async () => {
    render(<SurfaceBar breakdown={{ paved_m: 2000, unpaved_m: 1000, track_m: 1000 }} country="IN" />);
    const bar = screen.getByRole("button");
    expect(bar).toHaveAccessibleName(/Paved 2.0 km, Kaccha road 1.0 km, Track 1.0 km/);
    const segs = bar.querySelectorAll("span > span");
    expect((segs[0] as HTMLElement).style.width).toBe("50%");
    expect((segs[1] as HTMLElement).style.width).toBe("25%");
    await userEvent.click(bar);
    expect(screen.getByRole("list")).toHaveTextContent("Track");
  });
});

describe("ConfidenceChip", () => {
  it("bands confidence like the API", () => {
    expect(bandOf(0.75)).toBe("high");
    expect(bandOf(0.6)).toBe("medium");
    expect(bandOf(0.49)).toBe("low");
  });
  it("shows band, age and riders", () => {
    render(<ConfidenceChip confidence={0.82} lastConfirmed={new Date(Date.now() - 86400000).toISOString()} riders={1} />);
    expect(screen.getByText(/High · Confirmed yesterday by 1 rider/)).toBeInTheDocument();
  });
});
